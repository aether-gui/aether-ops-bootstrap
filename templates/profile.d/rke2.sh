# Added by aether-ops-bootstrap
# Makes kubectl, helm, and RKE2 tools available to all login shells.
# Users in the aether-ops group can use kubectl directly.
# Other users can use: sudo kubectl ...

export KUBECONFIG=/etc/rancher/rke2/rke2.yaml
export PATH="${PATH}:/var/lib/rancher/rke2/bin:/usr/local/bin"
