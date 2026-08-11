// Package factory holds the canonical type→adapter construction seam.
//
// It exists because the mapping used to live in two places with divergent
// coverage — a runtime switch in internal/api and a hand-rolled per-type pass in
// the boot path (cmd/joe) — so which components Joe could actually connect
// depended on which path had run. A component of a type only the boot pass knew
// got a silent nil at promotion; a component of a type only the runtime switch
// knew lost its adapter on every restart. The two sets are now one set, and both
// construction paths route through New.
//
// The package sits below internal/api rather than inside it because the boot
// path cannot import an HTTP package's unexported helper, and beside the adapter
// packages rather than in internal/adapters because every adapter package
// imports internal/adapters for the Adapter interface — a constructor there
// would close an import cycle.
//
// Construction only. Deciding what to do with a live adapter afterwards is not
// this package's business: the query-time routing in internal/access keys on the
// Go interface type, and the refresh routing in internal/coreagent type-asserts
// an already-registered adapter. Neither builds anything, and neither belongs
// here.
package factory

import (
	"errors"
	"fmt"

	"github.com/jaimegago/joe/internal/adapters"
	alertmanageradapter "github.com/jaimegago/joe/internal/adapters/alerting/alertmanager"
	grafanaadapter "github.com/jaimegago/joe/internal/adapters/alerting/grafana"
	pagerdutyadapter "github.com/jaimegago/joe/internal/adapters/alerting/pagerduty"
	awsadapter "github.com/jaimegago/joe/internal/adapters/aws"
	azureadapter "github.com/jaimegago/joe/internal/adapters/azure"
	elasticsearchadapter "github.com/jaimegago/joe/internal/adapters/datastore/elasticsearch"
	kafkaadapter "github.com/jaimegago/joe/internal/adapters/datastore/kafka"
	mongodbadapter "github.com/jaimegago/joe/internal/adapters/datastore/mongodb"
	mysqladapter "github.com/jaimegago/joe/internal/adapters/datastore/mysql"
	postgresadapter "github.com/jaimegago/joe/internal/adapters/datastore/postgres"
	redisadapter "github.com/jaimegago/joe/internal/adapters/datastore/redis"
	gitadapter "github.com/jaimegago/joe/internal/adapters/git"
	githubadapter "github.com/jaimegago/joe/internal/adapters/github"
	gitlabadapter "github.com/jaimegago/joe/internal/adapters/gitlab"
	argocdadapter "github.com/jaimegago/joe/internal/adapters/gitops/argocd"
	terraformadapter "github.com/jaimegago/joe/internal/adapters/iac/terraform"
	"github.com/jaimegago/joe/internal/adapters/k8s"
	envoyadapter "github.com/jaimegago/joe/internal/adapters/networking/envoy"
	nginxadapter "github.com/jaimegago/joe/internal/adapters/networking/nginx"
	datadogadapter "github.com/jaimegago/joe/internal/adapters/observability/datadog"
	dynatraceadapter "github.com/jaimegago/joe/internal/adapters/observability/dynatrace"
	jaegeradapter "github.com/jaimegago/joe/internal/adapters/observability/jaeger"
	lokiadapter "github.com/jaimegago/joe/internal/adapters/observability/loki"
	newrelicadapter "github.com/jaimegago/joe/internal/adapters/observability/newrelic"
	prometheusadapter "github.com/jaimegago/joe/internal/adapters/observability/prometheus"
	splunkadapter "github.com/jaimegago/joe/internal/adapters/observability/splunk"
	tempoadapter "github.com/jaimegago/joe/internal/adapters/observability/tempo"
	helmadapter "github.com/jaimegago/joe/internal/adapters/packaging/helm"
	falcoadapter "github.com/jaimegago/joe/internal/adapters/security/falco"
	"github.com/jaimegago/joe/internal/store"
)

// ErrNoAdapter reports that no adapter is wired for a component type. For a type
// in store.AllowedComponentTypes this is a coverage bug and nothing else — a
// registrable type is one an operator can create through the governed path, so
// reaching this for one means a component exists that Joe can never connect.
// TestNew_CoversEveryRegistrableComponentType is what should catch that, ahead of
// any caller; the error is what a caller surfaces if it ever does not.
//
// It is NOT the error for a failed connection. Connect failing on an adapter that
// was built is an expected runtime condition with its own handling at each call
// site, and the two must not be conflated.
var ErrNoAdapter = errors.New("no adapter wired for component type")

// New returns a fresh, unconnected adapter for the given component type.
//
// This is the single source of truth for the type→adapter mapping. Unlike the
// comment it replaces, that is now a checkable claim rather than an aspiration:
// the boot connect pass (connectSourcesDefault, cmd/joe/server.go), the
// promotion activation path (connectAndRegisterAdapter, internal/api/components.go)
// and the Test Connection handler (handleTestComponent, internal/api/webui.go)
// all call it, and nothing else builds an adapter from a type string.
//
// Coverage is the union of the two construction sets this replaced, so it admits
// types that are no longer registrable (see the UNREGISTRABLE note in
// internal/store/constants.go). That is deliberate: registration can no longer
// create such a row, but rows stored before the trims still exist, and a stored
// component whose adapter Joe declines to build is a component Joe cannot reach.
// Registrability is enforced at the registration seam, not here.
//
// The artifact-registry types are the one group the union does not reach. They
// have never had a construction path, which is what made them unregistrable under
// D-0058, and wiring them is deferred to
// docs/backlog/trim-deadonarrival-component-types.md. They return ErrNoAdapter.
func New(componentType string) (adapters.Adapter, error) {
	switch componentType {
	case store.ComponentTypeAWS:
		return awsadapter.New(), nil
	case store.ComponentTypeAzure:
		return azureadapter.New(), nil
	case store.ComponentTypeKubernetes:
		return k8s.New(), nil
	case store.ComponentTypeGit:
		return gitadapter.New(), nil
	case store.ComponentTypePrometheus, store.ComponentTypeMimir:
		return prometheusadapter.New(), nil
	case store.ComponentTypeLoki:
		return lokiadapter.New(), nil
	case store.ComponentTypeTempo:
		return tempoadapter.New(), nil
	case store.ComponentTypeJaeger:
		return jaegeradapter.New(), nil
	case store.ComponentTypeDatadog:
		return datadogadapter.New(), nil
	case store.ComponentTypeSplunk:
		return splunkadapter.New(), nil
	case store.ComponentTypeDynatrace:
		return dynatraceadapter.New(), nil
	case store.ComponentTypeNewRelic:
		return newrelicadapter.New(), nil
	case store.ComponentTypeAlertmanager:
		return alertmanageradapter.New(), nil
	case store.ComponentTypePagerDuty:
		return pagerdutyadapter.New(), nil
	case store.ComponentTypeGrafana:
		return grafanaadapter.New(), nil
	case store.ComponentTypePostgreSQL:
		return postgresadapter.New(), nil
	case store.ComponentTypeMySQL:
		return mysqladapter.New(), nil
	case store.ComponentTypeRedis:
		return redisadapter.New(), nil
	case store.ComponentTypeMongoDB:
		return mongodbadapter.New(), nil
	case store.ComponentTypeKafka:
		return kafkaadapter.New(), nil
	case store.ComponentTypeElasticsearch:
		return elasticsearchadapter.New(), nil
	case store.ComponentTypeArgoCd:
		return argocdadapter.New(), nil
	case store.ComponentTypeTerraform:
		return terraformadapter.New(), nil
	case store.ComponentTypeHelm:
		return helmadapter.New(), nil
	case store.ComponentTypeNginx:
		return nginxadapter.New(), nil
	case store.ComponentTypeEnvoy:
		return envoyadapter.New(), nil
	case store.ComponentTypeFalco:
		return falcoadapter.New(), nil
	case store.ComponentTypeGitHub:
		return githubadapter.New(), nil
	case store.ComponentTypeGitLab:
		return gitlabadapter.New(), nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrNoAdapter, componentType)
	}
}
