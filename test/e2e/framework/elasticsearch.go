package framework

import (
	"fmt"
	"strconv"
	"time"

	"github.com/appscode/go/crypto/rand"
	jtypes "github.com/appscode/go/encoding/json/types"
	"github.com/appscode/go/types"
	. "github.com/onsi/gomega"
	core "k8s.io/api/core/v1"
	policy "k8s.io/api/policy/v1beta1"
	kerr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	meta_util "kmodules.xyz/client-go/meta"
	api "kubedb.dev/apimachinery/apis/kubedb/v1alpha1"
	util "kubedb.dev/apimachinery/client/clientset/versioned/typed/kubedb/v1alpha1/util"
	"kubedb.dev/elasticsearch/pkg/util/es"
)

var (
	JobPvcStorageSize = "2Gi"
	DBPvcStorageSize  = "1Gi"
)

const (
	kindEviction = "Eviction"
)

func (i *Invocation) CombinedElasticsearch() *api.Elasticsearch {
	return &api.Elasticsearch{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rand.WithUniqSuffix("elasticsearch"),
			Namespace: i.namespace,
			Labels: map[string]string{
				"app": i.app,
			},
		},
		Spec: api.ElasticsearchSpec{
			Version:   jtypes.StrYo(DBCatalogName),
			Replicas:  types.Int32P(1),
			EnableSSL: true,
			Storage: &core.PersistentVolumeClaimSpec{
				Resources: core.ResourceRequirements{
					Requests: core.ResourceList{
						core.ResourceStorage: resource.MustParse(DBPvcStorageSize),
					},
				},
				StorageClassName: types.StringP(i.StorageClass),
			},
		},
	}
}

func (i *Invocation) DedicatedElasticsearch() *api.Elasticsearch {
	return &api.Elasticsearch{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rand.WithUniqSuffix("elasticsearch"),
			Namespace: i.namespace,
			Labels: map[string]string{
				"app": i.app,
			},
		},
		Spec: api.ElasticsearchSpec{
			Version: jtypes.StrYo(DBCatalogName),
			Topology: &api.ElasticsearchClusterTopology{
				Master: api.ElasticsearchNode{
					Replicas: types.Int32P(2),
					Prefix:   "master",
					Storage: &core.PersistentVolumeClaimSpec{
						Resources: core.ResourceRequirements{
							Requests: core.ResourceList{
								core.ResourceStorage: resource.MustParse(DBPvcStorageSize),
							},
						},
						StorageClassName: types.StringP(i.StorageClass),
					},
				},
				Data: api.ElasticsearchNode{
					Replicas: types.Int32P(2),
					Prefix:   "data",
					Storage: &core.PersistentVolumeClaimSpec{
						Resources: core.ResourceRequirements{
							Requests: core.ResourceList{
								core.ResourceStorage: resource.MustParse(DBPvcStorageSize),
							},
						},
						StorageClassName: types.StringP(i.StorageClass),
					},
				},
				Client: api.ElasticsearchNode{
					Replicas: types.Int32P(2),
					Prefix:   "client",
					Storage: &core.PersistentVolumeClaimSpec{
						Resources: core.ResourceRequirements{
							Requests: core.ResourceList{
								core.ResourceStorage: resource.MustParse(DBPvcStorageSize),
							},
						},
						StorageClassName: types.StringP(i.StorageClass),
					},
				},
			},
			EnableSSL: true,
		},
	}
}

func (f *Framework) CreateElasticsearch(obj *api.Elasticsearch) error {
	_, err := f.dbClient.KubedbV1alpha1().Elasticsearches(obj.Namespace).Create(obj)
	return err
}

func (f *Framework) GetElasticsearch(meta metav1.ObjectMeta) (*api.Elasticsearch, error) {
	return f.dbClient.KubedbV1alpha1().Elasticsearches(meta.Namespace).Get(meta.Name, metav1.GetOptions{})
}

func (f *Framework) TryPatchElasticsearch(meta metav1.ObjectMeta, transform func(*api.Elasticsearch) *api.Elasticsearch) (*api.Elasticsearch, error) {
	elasticsearch, err := f.dbClient.KubedbV1alpha1().Elasticsearches(meta.Namespace).Get(meta.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	elasticsearch, _, err = util.PatchElasticsearch(f.dbClient.KubedbV1alpha1(), elasticsearch, transform)
	return elasticsearch, err
}

func (f *Framework) DeleteElasticsearch(meta metav1.ObjectMeta) error {
	return f.dbClient.KubedbV1alpha1().Elasticsearches(meta.Namespace).Delete(meta.Name, nil)
}

func (f *Framework) EventuallyElasticsearch(meta metav1.ObjectMeta) GomegaAsyncAssertion {
	return Eventually(
		func() bool {
			_, err := f.dbClient.KubedbV1alpha1().Elasticsearches(meta.Namespace).Get(meta.Name, metav1.GetOptions{})
			if err != nil {
				if kerr.IsNotFound(err) {
					return false
				} else {
					Expect(err).NotTo(HaveOccurred())
				}
			}
			return true
		},
		time.Minute*5,
		time.Second*5,
	)
}

func (f *Framework) EventuallyElasticsearchPhase(meta metav1.ObjectMeta) GomegaAsyncAssertion {
	return Eventually(
		func() api.DatabasePhase {
			db, err := f.dbClient.KubedbV1alpha1().Elasticsearches(meta.Namespace).Get(meta.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			return db.Status.Phase
		},
		time.Minute*5,
		time.Second*5,
	)
}

func (f *Framework) EventuallyElasticsearchRunning(meta metav1.ObjectMeta) GomegaAsyncAssertion {
	return Eventually(
		func() bool {
			elasticsearch, err := f.dbClient.KubedbV1alpha1().Elasticsearches(meta.Namespace).Get(meta.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			return elasticsearch.Status.Phase == api.DatabasePhaseRunning
		},
		time.Minute*15,
		time.Second*10,
	)
}

func (f *Framework) EventuallyElasticsearchClientReady(meta metav1.ObjectMeta) GomegaAsyncAssertion {
	return Eventually(
		func() bool {
			client, err := f.GetElasticClient(meta)
			if err != nil {
				return false
			}
			client.Stop()
			f.Tunnel.Close()
			return true
		},
		time.Minute*15,
		time.Second*5,
	)
}

func (f *Framework) EventuallyElasticsearchIndicesCount(client es.ESClient) GomegaAsyncAssertion {
	return Eventually(
		func() int {
			count, err := client.CountIndex()
			if err != nil {
				return -1
			}
			return count
		},
		time.Minute*10,
		time.Second*5,
	)
}

func (f *Framework) CleanElasticsearch() {
	elasticsearchList, err := f.dbClient.KubedbV1alpha1().Elasticsearches(f.namespace).List(metav1.ListOptions{})
	if err != nil {
		return
	}
	for _, e := range elasticsearchList.Items {
		if _, _, err := util.PatchElasticsearch(f.dbClient.KubedbV1alpha1(), &e, func(in *api.Elasticsearch) *api.Elasticsearch {
			in.ObjectMeta.Finalizers = nil
			in.Spec.TerminationPolicy = api.TerminationPolicyWipeOut
			return in
		}); err != nil {
			fmt.Printf("error Patching Elasticsearch. error: %v", err)
		}
	}
	if err := f.dbClient.KubedbV1alpha1().Elasticsearches(f.namespace).DeleteCollection(deleteInForeground(), metav1.ListOptions{}); err != nil {
		fmt.Printf("error in deletion of Elasticsearch. Error: %v", err)
	}
}

func (f *Framework) EvictPodsFromStatefulSet(meta metav1.ObjectMeta) error {
	var err error
	labelSelector := labels.Set{
		meta_util.ManagedByLabelKey: api.GenericKey,
		api.LabelDatabaseKind:       api.ResourceKindElasticsearch,
		api.LabelDatabaseName:       meta.GetName(),
	}
	// get sts in the namespace
	stsList, err := f.kubeClient.AppsV1().StatefulSets(meta.Namespace).List(metav1.ListOptions{LabelSelector: labelSelector.String()})
	if err != nil {
		return err
	}
	for _, sts := range stsList.Items {
		// if PDB is not found, send error
		var pdb *policy.PodDisruptionBudget
		pdb, err = f.kubeClient.PolicyV1beta1().PodDisruptionBudgets(sts.Namespace).Get(sts.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		eviction := &policy.Eviction{
			TypeMeta: metav1.TypeMeta{
				APIVersion: policy.SchemeGroupVersion.String(),
				Kind:       kindEviction,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      sts.Name,
				Namespace: sts.Namespace,
			},
			DeleteOptions: &metav1.DeleteOptions{},
		}

		if pdb.Spec.MaxUnavailable == nil {
			return fmt.Errorf("found pdb %s spec.maxUnavailable nil", pdb.Name)
		}

		// try to evict as many pod as allowed in pdb. No err should occur
		maxUnavailable := pdb.Spec.MaxUnavailable.IntValue()
		for i := 0; i < maxUnavailable; i++ {
			eviction.Name = sts.Name + "-" + strconv.Itoa(i)

			err := f.kubeClient.PolicyV1beta1().Evictions(eviction.Namespace).Evict(eviction)
			if err != nil {
				return err
			}
		}

		// try to evict one extra pod. TooManyRequests err should occur
		eviction.Name = sts.Name + "-" + strconv.Itoa(maxUnavailable)
		err = f.kubeClient.PolicyV1beta1().Evictions(eviction.Namespace).Evict(eviction)
		if kerr.IsTooManyRequests(err) {
			err = nil
		} else if err != nil {
			return err
		} else {
			return fmt.Errorf("expected pod %s/%s to be not evicted due to pdb %s", sts.Namespace, eviction.Name, pdb.Name)
		}
	}
	return err
}
