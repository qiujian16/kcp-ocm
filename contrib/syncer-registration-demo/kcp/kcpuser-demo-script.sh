DEMO_DIR="$(dirname "${BASH_SOURCE[0]}")"
. ../${DEMO_DIR}/demo-magic

function comment() {
  echo -e '\033[0;33m>>> '$1' <<<\033[0m'
}

clear

comment "As a developer, I link my ManagedClusterSet to my KCP workspace"
pe "kubectl annotate managedclusterset dev \"kcp-workspace=workspace1\" --overwrite"
