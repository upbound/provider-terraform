package identity

import (
	"context"
	"os"
	"sort"
	"strings"

	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/pkg/errors"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// Error strings.
const (
	errAddInformerToManager         = "cannot add informer factory to manager"
	errDeploymentEnvVarsNotSet      = "POD_NAMESPACE or POD_NAME environment variable is not set"
	errGetCurrentPod                = "cannot get current pod"
	errInitKubernetesInformerClient = "cannot init Kubernetes informer client"
	errListPods                     = "cannot list pods"
	errSetupPodInformer             = "cannot setup Pod informer"
	errSetupReplicaSetInformer      = "cannot setup ReplicaSet informer"
)

// Label strings.
const (
	labelCrossplanePackageRevision = "pkg.crossplane.io/revision"
)

var logger logging.Logger

type Identity interface {
	GetIndex() int
	GetReplicas() int
}

type IdentityHolder struct {
	index    int
	replicas int
}

func (i *IdentityHolder) GetIndex() int {
	return i.index
}

func (i *IdentityHolder) GetReplicas() int {
	return i.replicas
}

func Setup(mgr ctrl.Manager, o controller.Options) (Identity, error) {
	logger = o.Logger

	identity := &IdentityHolder{
		index:    -1,
		replicas: -1,
	}

	namespace := strings.TrimSpace(os.Getenv("POD_NAMESPACE"))
	podName := strings.TrimSpace(os.Getenv("POD_NAME"))
	if namespace == "" || podName == "" {
		return nil, errors.New(errDeploymentEnvVarsNotSet)
	}

	clientset, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		return nil, errors.Wrap(err, errInitKubernetesInformerClient)
	}

	rsName := ""
	if pod, err := clientset.CoreV1().Pods(namespace).Get(context.Background(), podName, metav1.GetOptions{}); err != nil {
		return nil, errors.Wrap(err, errGetCurrentPod)
	} else {
		rsName = pod.OwnerReferences[0].Name
	}

	informerFactory := informers.NewSharedInformerFactoryWithOptions(clientset, 0, informers.WithNamespace(namespace))
	if err := setupReplicaSetInformer(informerFactory, identity, rsName); err != nil {
		return nil, errors.Wrap(err, errSetupReplicaSetInformer)
	}
	if err := setupPodInformer(informerFactory, identity, rsName, podName); err != nil {
		return nil, errors.Wrap(err, errSetupPodInformer)
	}

	if err := mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		logger.Debug("Starting informers")
		informerFactory.Start(ctx.Done())
		informerFactory.WaitForCacheSync(ctx.Done())

		<-ctx.Done()
		logger.Debug("Stopping informers")
		return nil
	})); err != nil {
		return nil, errors.Wrap(err, errAddInformerToManager)
	}

	return identity, nil
}

func setupReplicaSetInformer(informerFactory informers.SharedInformerFactory, identityHolder *IdentityHolder, rsName string) error {
	_, err := informerFactory.Apps().V1().ReplicaSets().Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: replicaSetFilterFunc(rsName),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: replicaSetHandlerFunc(identityHolder),
			UpdateFunc: func(oldObj, newObj interface{}) {
				replicaSetHandlerFunc(identityHolder)(newObj)
			},
		},
	})
	return err
}

func replicaSetFilterFunc(rsName string) func(obj interface{}) bool {
	return func(obj interface{}) bool {
		return obj.(*appsv1.ReplicaSet).GetName() == rsName
	}
}

func replicaSetHandlerFunc(identityHolder *IdentityHolder) func(obj interface{}) {
	return func(obj interface{}) {
		identityHolder.replicas = int(*obj.(*appsv1.ReplicaSet).Spec.Replicas)
		logger.Debug("Replicas value updated", "replicas", identityHolder.replicas)
	}
}

func setupPodInformer(informerFactory informers.SharedInformerFactory, identityHolder *IdentityHolder, rsName string, podName string) error {
	_, err := informerFactory.Core().V1().Pods().Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: podFilterFunc(rsName),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    podHandlerFunc(informerFactory, identityHolder, podName),
			DeleteFunc: podHandlerFunc(informerFactory, identityHolder, podName),
		},
	})
	return err
}

func podFilterFunc(rsName string) func(obj interface{}) bool {
	return func(obj interface{}) bool {
		for _, ownerRef := range obj.(*corev1.Pod).GetOwnerReferences() {
			if ownerRef.Name == rsName {
				return true
			}
		}
		return false
	}
}

func podHandlerFunc(informerFactory informers.SharedInformerFactory, identityHolder *IdentityHolder, podName string) func(obj interface{}) {
	return func(obj interface{}) {
		identityHolder.index = -1

		pods, err := informerFactory.Core().V1().Pods().Lister().
			List(labels.Set{labelCrossplanePackageRevision: obj.(*corev1.Pod).GetLabels()[labelCrossplanePackageRevision]}.AsSelector())
		if err != nil {
			logger.Info(errListPods, "error", err)
			return
		}

		sort.Slice(pods, func(i, j int) bool {
			return pods[i].Name < pods[j].Name
		})
		for i, pod := range pods {
			if pod.Name == podName {
				identityHolder.index = i
				break
			}
		}

		logger.Debug("Index value updated", "index", identityHolder.index)
	}
}
