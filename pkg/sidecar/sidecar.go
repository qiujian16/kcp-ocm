package sidecar

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"
)

const (
	kcpKubeconfigName       = "admin.kubeconfig"
	kcpKubeconfigSecretName = "kcp-admin-kubeconfig"
)

type SidecarOptions struct {
	RootDirectory string
}

func (o *SidecarOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.RootDirectory, "root-directory", o.RootDirectory, "KCP root directory.")
}

func (o *SidecarOptions) Complete() error {
	if o.RootDirectory == "" {
		return fmt.Errorf("KCP root directory is required")
	}

	return nil
}

func (o *SidecarOptions) Run(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	if err := o.Complete(); err != nil {
		klog.Fatal(err)
	}

	kubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	namespacedKubeInformerFactory := informers.NewSharedInformerFactoryWithOptions(kubeClient, 10*time.Minute, informers.WithNamespace(controllerContext.OperatorNamespace))

	ctrl := NewKCPKubeconfigSecretController(
		kubeClient.CoreV1(),
		controllerContext.OperatorNamespace,
		o.RootDirectory,
		namespacedKubeInformerFactory.Core().V1().Secrets(),
		controllerContext.EventRecorder,
	)

	go namespacedKubeInformerFactory.Start(ctx.Done())

	go ctrl.Run(ctx, 1)
	<-ctx.Done()
	return nil
}

type kcpKubeconfigSecretController struct {
	kubeCoreClient               corev1client.CoreV1Interface
	kcpKubeconfigSecretNamespace string
	kcpKubeconfigDirectory       string
}

func NewKCPKubeconfigSecretController(
	kubeCoreClient corev1client.CoreV1Interface,
	kcpKubeconfigSecretNamespace, kcpKubeconfigDirectory string,
	secretInformer corev1informers.SecretInformer,
	recorder events.Recorder) factory.Controller {
	s := &kcpKubeconfigSecretController{
		kubeCoreClient:               kubeCoreClient,
		kcpKubeconfigSecretNamespace: kcpKubeconfigSecretNamespace,
		kcpKubeconfigDirectory:       kcpKubeconfigDirectory,
	}

	return factory.New().
		WithFilteredEventsInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				accessor, _ := meta.Accessor(obj)
				return accessor.GetName()
			},
			func(obj interface{}) bool {
				accessor, err := meta.Accessor(obj)
				if err != nil {
					return false
				}
				if accessor.GetNamespace() == kcpKubeconfigSecretNamespace && accessor.GetName() == kcpKubeconfigSecretName {
					return true
				}
				return false
			}, secretInformer.Informer()).
		WithSync(s.sync).
		ResyncEvery(5*time.Minute).
		ToController("KCPKubeconfigSecretController", recorder)
}

func (s *kcpKubeconfigSecretController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	return DumpSecret(ctx, s.kubeCoreClient, s.kcpKubeconfigSecretNamespace, s.kcpKubeconfigDirectory, syncCtx.Recorder())
}

func DumpSecret(ctx context.Context,
	kubeCoreClient corev1client.CoreV1Interface,
	secretNamespace, kcpKubeconfigDirectory string,
	recorder events.Recorder) error {
	kcpKubeconfigFilePath := path.Clean(path.Join(kcpKubeconfigDirectory, kcpKubeconfigName))
	if _, err := os.Stat(kcpKubeconfigFilePath); err != nil {
		return err
	}

	kcpRestConfig, err := clientcmd.BuildConfigFromFlags("", kcpKubeconfigFilePath)
	if err != nil {
		return err
	}

	kubeconfigBytes, err := clientcmd.Write(buildKCPAdminKubeconfig(kcpRestConfig, secretNamespace))
	if err != nil {
		return err
	}

	secret, err := kubeCoreClient.Secrets(secretNamespace).Get(ctx, kcpKubeconfigSecretName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		_, err := kubeCoreClient.Secrets(secretNamespace).Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: kcpKubeconfigSecretName, Namespace: secretNamespace},
			Data:       map[string][]byte{kcpKubeconfigName: kubeconfigBytes},
		}, metav1.CreateOptions{})

		return err
	}
	if err != nil {
		return err
	}

	last, ok := secret.Data[kcpKubeconfigName]
	if ok && bytes.Equal(kubeconfigBytes, last) {
		return nil
	}

	secret.Data[kcpKubeconfigName] = kubeconfigBytes
	_, err = kubeCoreClient.Secrets(secretNamespace).Update(ctx, secret, metav1.UpdateOptions{})
	return err
}

func buildKCPAdminKubeconfig(restConfig *rest.Config, namespace string) clientcmdapi.Config {
	return clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{"admin": {
			// Server:                   fmt.Sprintf("https://kcp.%s.svc:6443", namespace),
			Server:                   "https://10.0.118.32:6443",
			CertificateAuthorityData: restConfig.CAData,
			TLSServerName:            restConfig.TLSClientConfig.ServerName,
		}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"loopback": {
			Token: restConfig.BearerToken,
		}},
		Contexts: map[string]*clientcmdapi.Context{"admin": {
			Cluster:  "admin",
			AuthInfo: "loopback",
		}},
		CurrentContext: "admin",
	}
}
