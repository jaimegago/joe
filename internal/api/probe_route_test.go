package api

import (
	"fmt"
	"net/http"

	"github.com/jaimegago/joe/internal/rbac"
)

// RegisterProbeRouteForTest registers a single minimal protected route that
// exercises the production auth chain — EdgeAuth credential extraction →
// context principal → guarded accessor read decision → status mapping — WITHOUT
// depending on any real product route.
//
// It deliberately lives in a _test.go file: it is compiled ONLY into package
// api's test binary, is never referenced by Server.RegisterRoutes, and
// therefore never appears in the shipped binary's route table. `go build ./...`
// does not compile it. Exported here (rather than unexported) so the external
// api_test package — which holds the auth/RBAC regression suite — can mount it
// on its test mux.
//
// The handler reproduces exactly what the removed direct-HTTP k8s GET handler
// did for that suite: it reads the caller principal from the request context,
// asks the guarded accessor for a read on {componentID}, and maps the result
// with the shared handleAccessError (403 on ErrPermissionDenied, 404 on
// component-not-found, 200 on allow when a fake adapter is registered). The
// route path is synthetic (/probe/...), not a managed-system surface, so no
// test pins a production route as its fixture.
func (s *Server) RegisterProbeRouteForTest(mux *http.ServeMux) {
	mux.HandleFunc(fmt.Sprintf("GET %s/probe/{componentID}/read", apiPrefix), func(w http.ResponseWriter, r *http.Request) {
		componentID := r.PathValue("componentID")
		principal := rbac.PrincipalFromContext(r.Context())
		items, err := s.accessor.K8sListResources(r.Context(), principal, componentID, "pods", "")
		if err != nil {
			if handleAccessError(w, err, componentID, "k8s") {
				return
			}
			writeInternalError(w, err, "probe read")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"count": len(items)})
	})
}
