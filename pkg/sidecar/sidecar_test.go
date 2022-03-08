package sidecar

import (
	"io/ioutil"
	"os"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
)

func TestComplete(t *testing.T) {
	kcpKubeconfigFilePath := "/home/centos/test.yaml"
	if _, err := os.Stat(kcpKubeconfigFilePath); err != nil {
		return
	}

	kcpRestConfig, err := clientcmd.BuildConfigFromFlags("", kcpKubeconfigFilePath)
	if err != nil {
		return
	}

	kubeconfigBytes, err := clientcmd.Write(buildKCPAdminKubeconfig(kcpRestConfig, "test"))
	if err != nil {
		return
	}

	ioutil.WriteFile("/home/centos/test2.yaml", kubeconfigBytes, 0777)
}
