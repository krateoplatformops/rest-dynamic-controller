#!/bin/bash


kind get kubeconfig >/dev/null 2>&1 || kind create cluster

export COMPOSITION_CONTROLLER_DEBUG=true
export COMPOSITION_CONTROLLER_GROUP=composition.krateo.io
export COMPOSITION_CONTROLLER_VERSION=v0-1-0
export COMPOSITION_CONTROLLER_RESOURCE=fireworksapps
export COMPOSITION_CONTROLLER_NAMESPACE=demo-system

go run main.go -kubeconfig $HOME/.kube/config