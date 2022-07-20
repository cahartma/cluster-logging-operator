#!/bin/bash

export REMOTE_REGISTRY=true
export PUSH_USER=kubeadmin
export SKIP_BUILD=false
export LIB_VIRT=/home/cahartma/.crc/cache/crc_libvirt_4.9.0/
export KUBECONFIG=$LIB_VIRT/kubeconfig
export PUSH_PASSWORD=$( cat $LIB_VIRT/kubeadmin-password )
oc login -u kubeadmin -p $( cat $LIB_VIRT/kubeadmin-password ) https://api.crc.testing:6443
oc whoami

