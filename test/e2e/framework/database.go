package framework

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kmodules.xyz/client-go/tools/portforward"
	api "kubedb.dev/apimachinery/apis/kubedb/v1alpha1"
	amc "kubedb.dev/apimachinery/pkg/controller"
	"kubedb.dev/elasticsearch/pkg/controller"
	"kubedb.dev/elasticsearch/pkg/util/es"
)

func (f *Framework) GetClientPodName(elasticsearch *api.Elasticsearch) string {
	clientName := elasticsearch.Name

	if elasticsearch.Spec.Topology != nil {
		if elasticsearch.Spec.Topology.Client.Prefix != "" {
			clientName = fmt.Sprintf("%v-%v", elasticsearch.Spec.Topology.Client.Prefix, clientName)
		}
	}
	return fmt.Sprintf("%v-0", clientName)
}

func (f *Framework) GetElasticClient(meta metav1.ObjectMeta) (es.ESClient, error) {
	db, err := f.GetElasticsearch(meta)
	if err != nil {
		return nil, err
	}
	clientPodName := f.GetClientPodName(db)
	f.Tunnel = portforward.NewTunnel(
		f.kubeClient.CoreV1().RESTClient(),
		f.restConfig,
		db.Namespace,
		clientPodName,
		api.ElasticsearchRestPort,
	)
	if err := f.Tunnel.ForwardPort(); err != nil {
		return nil, err
	}
	url := fmt.Sprintf("%v://127.0.0.1:%d", db.GetConnectionScheme(), f.Tunnel.Local)
	c := controller.New(nil, f.kubeClient, nil, f.dbClient, nil, nil, nil, nil, nil, amc.Config{}, nil)
	return es.GetElasticClient(c.Client, c.ExtClient, db, url)
}
