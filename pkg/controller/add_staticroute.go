package controller

import (
	"github.com/IBM-Cloud/kube-samples/staticroute-operator/pkg/controller/staticroute"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, staticroute.Add)
}
