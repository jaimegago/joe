package runmodel_test

import (
	"reflect"

	"github.com/jaimegago/joe/internal/runmodel"
)

// repositoryReflectType returns the reflect.Type for the runmodel.Repository
// interface. Used by interface-shape tests in repository_test.go.
func repositoryReflectType() reflect.Type {
	return reflect.TypeOf((*runmodel.Repository)(nil)).Elem()
}
