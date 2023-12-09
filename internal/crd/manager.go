package crd

import (
	"context"
	"strings"

	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
)

// Manager is an object that allows for the management of CRDs
// It is mainly responsible for creating and deleting CRDs
type Manager struct {
	Client *apiextensionsv1.ApiextensionsV1Client
}

func NewManager(Client *apiextensionsv1.ApiextensionsV1Client) *Manager {
	return &Manager{
		Client: Client,
	}
}

func (m *Manager) Create(ctx context.Context, crd v1.CustomResourceDefinition) error {
	_, err := m.Client.CustomResourceDefinitions().Create(
		ctx,
		&crd,
		metav1.CreateOptions{},
	)
	return err
}

func (m *Manager) Delete(ctx context.Context, name string) error {
	err := m.Client.CustomResourceDefinitions().Delete(
		ctx,
		name,
		metav1.DeleteOptions{},
	)
	return err
}

func FromOpenAPIV3Schema(apiVersion, kind string, schema *v1.JSONSchemaProps) v1.CustomResourceDefinition {
	return v1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: strings.ToLower(kind) + "s.x.symphony.k8s.aws",
		},
		Spec: v1.CustomResourceDefinitionSpec{
			Group: "x.symphony.k8s.aws",
			Names: v1.CustomResourceDefinitionNames{
				Kind:     kind,
				ListKind: kind + "List",
				Plural:   strings.ToLower(kind) + "s",
				Singular: strings.ToLower(kind),
			},
			Scope: v1.NamespaceScoped,
			Versions: []v1.CustomResourceDefinitionVersion{
				{
					Name:    apiVersion,
					Served:  true,
					Storage: true,
					Schema: &v1.CustomResourceValidation{
						OpenAPIV3Schema: schema,
					},
				},
			},
		},
	}
}