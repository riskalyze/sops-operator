/*
Copyright The SOPS Operator Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"os"
	"testing"

	"go.uber.org/zap/zapcore"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	"github.com/riskalyze/sops-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	uberzap "go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type FakeDecryptor struct{}

func (f *FakeDecryptor) Decrypt(_ string, _ string) ([]byte, error) {
	return []byte("unencrypted"), nil
}

func TestMain(m *testing.M) {
	logf.SetLogger(
		zap.New(zap.UseDevMode(true),
			zap.Encoder(zapcore.NewConsoleEncoder(uberzap.NewDevelopmentEncoderConfig()))),
	)

	os.Exit(m.Run())
}

var (
	name      = "test-secret"
	namespace = "test-namespace"
	req       = reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}
)

func TestReconcile_Create(t *testing.T) {
	tests := []struct {
		name       string
		sopsSecret *v1alpha1.SopsSecret
	}{
		{
			name: "without metadata",
			sopsSecret: &v1alpha1.SopsSecret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: v1alpha1.SopsSecretSpec{
					StringData: map[string]string{"test.yaml": "encrypted"},
				},
			},
		},
		{
			name: "with metadata",
			sopsSecret: &v1alpha1.SopsSecret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: v1alpha1.SopsSecretSpec{
					Metadata: v1alpha1.SopsSecretObjectMeta{
						Labels:      map[string]string{"mylabel": "foo"},
						Annotations: map[string]string{"myannotation": "bar"},
					},
					StringData: map[string]string{"test.yaml": "encrypted"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := runtime.NewScheme()
			utilruntime.Must(scheme.AddToScheme(s))
			utilruntime.Must(v1alpha1.AddToScheme(s))

			recorder := record.NewFakeRecorder(1)
			r := newSopsSecretReconciler(s, recorder, tt.sopsSecret)

			res, err := r.Reconcile(context.Background(), req)
			require.NoError(t, err)
			assert.False(t, res.Requeue)

			secret := &corev1.Secret{}
			err = r.Get(context.Background(), req.NamespacedName, secret)
			require.NoError(t, err)
			assert.Equal(t, []byte("unencrypted"), secret.Data["test.yaml"])
			assert.Equal(t, tt.sopsSecret.Spec.Metadata.Labels, secret.Labels)
			assert.Equal(t, tt.sopsSecret.Spec.Metadata.Annotations, secret.Annotations)
			event := <-recorder.Events
			assert.Equal(t, event, "Normal Created Created secret: test-secret")
		})
	}
}

func TestReconcile_Update(t *testing.T) {
	sopsSecret := &v1alpha1.SopsSecret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	s := runtime.NewScheme()
	utilruntime.Must(scheme.AddToScheme(s))
	utilruntime.Must(v1alpha1.AddToScheme(s))

	recorder := record.NewFakeRecorder(2)
	r := newSopsSecretReconciler(s, recorder, sopsSecret)

	res, err := r.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, res.Requeue)
	event := <-recorder.Events
	assert.Equal(t, event, "Normal Created Created secret: test-secret")

	secret := &corev1.Secret{}
	err = r.Get(context.Background(), req.NamespacedName, secret)
	require.NoError(t, err)
	assert.Empty(t, secret.Labels)
	assert.Empty(t, secret.Annotations)

	err = r.Get(context.Background(), req.NamespacedName, sopsSecret)
	require.NoError(t, err)

	sopsSecret.Spec.Metadata.Labels = map[string]string{
		"mylabel": "foo",
	}
	sopsSecret.Spec.Metadata.Annotations = map[string]string{
		"myannotation": "bar",
	}

	err = r.Update(context.Background(), sopsSecret)
	require.NoError(t, err)

	res, err = r.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, res.Requeue)
	err = r.Get(context.Background(), req.NamespacedName, secret)
	require.NoError(t, err)
	assert.Equal(t, sopsSecret.Spec.Metadata.Labels, secret.Labels)
	assert.Equal(t, sopsSecret.Spec.Metadata.Annotations, secret.Annotations)
	event = <-recorder.Events
	assert.Equal(t, event, "Normal Updated Updated secret: test-secret")
}

func TestExistingSecretNotOwnedByUs(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         namespace,
			CreationTimestamp: metav1.Now(),
		},
	}

	sopsSecret := &v1alpha1.SopsSecret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	s := runtime.NewScheme()
	utilruntime.Must(scheme.AddToScheme(s))
	utilruntime.Must(v1alpha1.AddToScheme(s))

	recorder := record.NewFakeRecorder(1)
	r := newSopsSecretReconciler(s, recorder, secret, sopsSecret)

	_, err := r.Reconcile(context.Background(), req)
	require.NoError(t, err)
	event := <-recorder.Events
	assert.Contains(t, event, "Secret already exists and not owned by sops-operator")

	err = r.Delete(context.Background(), secret)
	require.NoError(t, err)

	_, err = r.Reconcile(context.Background(), req)
	require.NoError(t, err)
	event = <-recorder.Events
	assert.Contains(t, event, "Normal Created Created secret: test-secret")
}

func newSopsSecretReconciler(s *runtime.Scheme, recorder *record.FakeRecorder, objs ...runtime.Object) *SopsSecretReconciler {
	cl := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()
	return &SopsSecretReconciler{
		Client:    cl,
		Scheme:    s,
		Recorder:  recorder,
		Decryptor: &FakeDecryptor{},
	}
}
