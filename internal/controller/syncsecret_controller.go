/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"time"

	syncv1 "github.com/clgcn/sync-credential-operator/api/v1"
)

// SyncSecretReconciler reconciles a SyncSecret object
type SyncSecretReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=sync.abroadme.me,resources=syncsecrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sync.abroadme.me,resources=syncsecrets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sync.abroadme.me,resources=syncsecrets/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the SyncSecret object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/reconcile
func (r *SyncSecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	syncSecret := &syncv1.SyncSecret{}

	if err := r.Get(ctx, req.NamespacedName, syncSecret); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// List all secrets in all namespaces
	secretList := &v1.SecretList{}

	if err := r.List(ctx, secretList); err != nil {
		return ctrl.Result{}, err
	}

	// Filter secrets by annotation
	annotationKey := syncSecret.Spec.AnnotationKey
	var annotatedSecret *v1.Secret

	for _, secret := range secretList.Items {
		if val, ok := secret.Annotations[annotationKey]; ok && val == "true" {
			annotatedSecret = &secret
			break
		}
	}

	if annotatedSecret == nil {
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
	}

	// Get all namespaces

	nsList := &v1.NamespaceList{}
	if err := r.List(ctx, nsList); err != nil {
		return ctrl.Result{}, err
	}

	excludeNamespaces := map[string]bool{
		"kube-node-lease": true,
		"kube-public":     true,
		"kube-system":     true,
	}

	for _, ns := range nsList.Items {
		if ns.Name == annotatedSecret.Namespace || excludeNamespaces[ns.Name] {
			continue
		}

		newSecret := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      annotatedSecret.Name,
				Namespace: ns.Name,
				Annotations: map[string]string{
					annotationKey: "true",
				},
			},
			Data:       annotatedSecret.Data,
			Type:       annotatedSecret.Type,
			StringData: annotatedSecret.StringData,
		}

		foundSecret := &v1.Secret{}
		err := r.Get(ctx, types.NamespacedName{Name: newSecret.Name, Namespace: ns.Name}, foundSecret)

		if err != nil && errors.IsNotFound(err) {
			if err := r.Create(ctx, newSecret); err != nil {
				return ctrl.Result{}, err
			}
		} else if err == nil {
			logger.Info("Staring to sync secret", "secret", foundSecret.Name)
			newSecret.ResourceVersion = foundSecret.ResourceVersion
			if err := r.Update(ctx, newSecret); err != nil {
				return ctrl.Result{}, err
			}
		} else {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SyncSecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&syncv1.SyncSecret{}).
		Owns(&v1.Secret{}).
		Complete(r)
}
