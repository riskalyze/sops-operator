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
	"maps"
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

type FakeDecryptorYaml struct{}

func (f *FakeDecryptorYaml) Decrypt(_ string, _ string) ([]byte, error) {
	return []byte("test-key: test-value\ntest-key-2: test-value-2"), nil
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
			assert.Zero(t, res.RequeueAfter)

			secret := &corev1.Secret{}
			err = r.Get(context.Background(), req.NamespacedName, secret)
			require.NoError(t, err)
			assert.Equal(t, []byte("unencrypted"), secret.Data["test.yaml"])
			assert.Equal(t, tt.sopsSecret.Spec.Metadata.Labels, secret.Labels)
			annotations := maps.Clone(secret.Annotations)
			delete(annotations, managedMetadataAnnotation)
			assert.Equal(t, tt.sopsSecret.Spec.Metadata.Annotations, annotations)
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
	assert.Zero(t, res.RequeueAfter)
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
	assert.Zero(t, res.RequeueAfter)
	err = r.Get(context.Background(), req.NamespacedName, secret)
	require.NoError(t, err)
	assert.Equal(t, sopsSecret.Spec.Metadata.Labels, secret.Labels)
	assert.Equal(t, "bar", secret.Annotations["myannotation"])
	assert.JSONEq(t, `{"annotations":["myannotation"],"labels":["mylabel"]}`, secret.Annotations[managedMetadataAnnotation])
	event = <-recorder.Events
	assert.Equal(t, event, "Normal Updated Updated secret: test-secret")
}

func TestReconcile_PreservesOtherControllerMetadata(t *testing.T) {
	for _, withMetadata := range []bool{false, true} {
		t.Run(map[bool]string{false: "without metadata", true: "with metadata"}[withMetadata], func(t *testing.T) {
			ctx := context.Background()
			s := runtime.NewScheme()
			utilruntime.Must(scheme.AddToScheme(s))
			utilruntime.Must(v1alpha1.AddToScheme(s))
			sopsSecret := &v1alpha1.SopsSecret{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
				Spec:       v1alpha1.SopsSecretSpec{StringData: map[string]string{"test.yaml": "encrypted"}},
			}
			if withMetadata {
				sopsSecret.Spec.Metadata = v1alpha1.SopsSecretObjectMeta{
					Annotations: map[string]string{"managed.example/key": "initial"},
					Labels:      map[string]string{"managed.example/key": "initial"},
				}
			}
			recorder := record.NewFakeRecorder(20)
			r := newSopsSecretReconciler(s, recorder, sopsSecret)
			_, err := r.Reconcile(ctx, req)
			require.NoError(t, err)
			require.Equal(t, "Normal Created Created secret: test-secret", <-recorder.Events)

			secret := &corev1.Secret{}
			require.NoError(t, r.Get(ctx, req.NamespacedName, secret))
			if secret.Annotations == nil {
				secret.Annotations = make(map[string]string)
			}
			if secret.Labels == nil {
				secret.Labels = make(map[string]string)
			}
			const hashKey = "percona.com/example-cluster-app-user-hash"
			secret.Annotations[hashKey] = "example-password-hash"
			secret.Labels["other.example/key"] = "external"
			require.NoError(t, r.Update(ctx, secret))
			before := secret.DeepCopy()
			require.NoError(t, r.Get(ctx, req.NamespacedName, sopsSecret))
			statusBefore := sopsSecret.Status

			// A Secret watch event must converge without another write or status update.
			for range 3 {
				_, err = r.Reconcile(ctx, req)
				require.NoError(t, err)
				require.NoError(t, r.Get(ctx, req.NamespacedName, secret))
				assert.Equal(t, before, secret)
				require.NoError(t, r.Get(ctx, req.NamespacedName, sopsSecret))
				assert.Equal(t, statusBefore, sopsSecret.Status)
				assert.Empty(t, recorder.Events)
			}

			if withMetadata {
				// Removing owned keys must still work without removing foreign keys.
				sopsSecret.Spec.Metadata = v1alpha1.SopsSecretObjectMeta{}
				require.NoError(t, r.Update(ctx, sopsSecret))
				_, err = r.Reconcile(ctx, req)
				require.NoError(t, err)
				require.NoError(t, r.Get(ctx, req.NamespacedName, secret))
				assert.Equal(t, map[string]string{hashKey: "example-password-hash"}, secret.Annotations)
				assert.Equal(t, map[string]string{"other.example/key": "external"}, secret.Labels)
				assert.Equal(t, before.Data, secret.Data)
				require.Equal(t, "Normal Updated Updated secret: test-secret", <-recorder.Events)
				before = secret.DeepCopy()
				_, err = r.Reconcile(ctx, req)
				require.NoError(t, err)
				require.NoError(t, r.Get(ctx, req.NamespacedName, secret))
				assert.Equal(t, before, secret)
				assert.Empty(t, recorder.Events)
			}
		})
	}
}

func TestSecretMetadata_AdoptsExistingSecret(t *testing.T) {
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Annotations: map[string]string{"external": "keep", "managed": "old"},
		Labels:      map[string]string{"external": "keep", "managed": "old"},
	}}
	desired := v1alpha1.SopsSecretObjectMeta{
		Annotations: map[string]string{"managed": "new"},
		Labels:      map[string]string{"managed": "new"},
	}
	require.NoError(t, reconcileSecretMetadata(secret, desired))
	assert.Equal(t, "keep", secret.Annotations["external"])
	assert.Equal(t, "new", secret.Annotations["managed"])
	assert.Equal(t, map[string]string{"external": "keep", "managed": "new"}, secret.Labels)
	assert.JSONEq(t, `{"annotations":["managed"],"labels":["managed"]}`, secret.Annotations[managedMetadataAnnotation])
	// Neither the informer-cached spec nor its maps should be modified.
	assert.Equal(t, map[string]string{"managed": "new"}, desired.Annotations)
	assert.Equal(t, map[string]string{"managed": "new"}, desired.Labels)
}

func TestSecretMetadata_InvalidOwnership(t *testing.T) {
	for _, tc := range []struct {
		name    string
		raw     string
		desired map[string]string
	}{
		{name: "malformed tracking annotation", raw: "not-json"},
		{name: "reserved annotation in spec", raw: `{}`, desired: map[string]string{managedMetadataAnnotation: "override"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{managedMetadataAnnotation: tc.raw, "external": "keep"},
				Labels:      map[string]string{"external": "keep"},
			}}
			before := secret.DeepCopy()
			err := reconcileSecretMetadata(secret, v1alpha1.SopsSecretObjectMeta{Annotations: tc.desired})
			require.Error(t, err)
			assert.Equal(t, before, secret)
		})
	}
}

func TestReconcile_right(t *testing.T) {
	assert.Equal(t, right("test.env.yaml", 9), ".env.yaml")
	assert.Equal(t, right("test.env.yml", 8), ".env.yml")
}

func TestReconcile_Update_MapValues(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         namespace,
			CreationTimestamp: metav1.Now(),
		},
	}

	sopsSecret := &v1alpha1.SopsSecret{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         namespace,
			CreationTimestamp: metav1.Now(),
		},
		Spec: v1alpha1.SopsSecretSpec{
			StringData: map[string]string{
				"test.env.yaml": "encrypted",
			},
		},
	}

	s := runtime.NewScheme()
	utilruntime.Must(scheme.AddToScheme(s))
	utilruntime.Must(v1alpha1.AddToScheme(s))

	recorder := record.NewFakeRecorder(2)
	r := newSopsSecretReconcilerYaml(s, recorder, sopsSecret)

	_ = r.update(context.Background(), secret, sopsSecret)

	assert.Equal(t, secret.Data["test-key"], []byte("test-value"))
	assert.Equal(t, secret.Data["test-key-2"], []byte("test-value-2"))
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
	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithStatusSubresource(&v1alpha1.SopsSecret{}).
		WithRuntimeObjects(objs...).
		Build()
	return &SopsSecretReconciler{
		Client:    cl,
		Scheme:    s,
		Recorder:  recorder,
		Decryptor: &FakeDecryptor{},
	}
}

func newSopsSecretReconcilerYaml(s *runtime.Scheme, recorder *record.FakeRecorder, objs ...runtime.Object) *SopsSecretReconciler {
	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithStatusSubresource(&v1alpha1.SopsSecret{}).
		WithRuntimeObjects(objs...).
		Build()
	return &SopsSecretReconciler{
		Client:    cl,
		Scheme:    s,
		Recorder:  recorder,
		Decryptor: &FakeDecryptorYaml{},
	}
}
