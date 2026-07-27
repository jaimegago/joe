package coreagent

import (
	crypto_rand "crypto/rand"
	"fmt"
	"testing"

	"github.com/jaimegago/joe/internal/componentgov"
	"github.com/jaimegago/joe/internal/store"
)

// TestRegisterComponentTool_GeneratedIDsPassValidation pins that every
// registrable component type yields a generated ID (the tool's
// type-hyphen-16-hex construction, replicated here) that passes the shared
// componentgov format rule. The tool asserts this post-generation at runtime;
// this test moves the failure to CI, so adding a type name that breaks the
// format (uppercase, underscore, over-length) fails here before it can fail
// in a live registration.
func TestRegisterComponentTool_GeneratedIDsPassValidation(t *testing.T) {
	types := store.AllowedComponentTypes()
	if len(types) == 0 {
		t.Fatal("AllowedComponentTypes returned an empty set; test is vacuous")
	}
	for _, sourceType := range types {
		t.Run(sourceType, func(t *testing.T) {
			randBytes := make([]byte, 8)
			if _, err := crypto_rand.Read(randBytes); err != nil {
				t.Fatalf("rand: %v", err)
			}
			id := fmt.Sprintf("%s-%x", sourceType, randBytes)
			if err := componentgov.ValidateComponentID(id); err != nil {
				t.Errorf("generated ID %q for type %q fails validation: %v", id, sourceType, err)
			}
		})
	}
}
