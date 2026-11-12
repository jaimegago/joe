package componentgov

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/jaimegago/joe/internal/credential"
)

// ErrInvalidRegistrationConfig is the sentinel every registration-contract
// rejection wraps. Callers surface it as a 4xx (HTTP) or a tool error (LLM).
var ErrInvalidRegistrationConfig = errors.New("invalid registration config")

// ValidateRegistrationConfig enforces a component type's declared registration
// contract (credential.RegistrationFields, D-0168) on a submitted config, for
// SHAPE only. It is the third shared registration seam beside
// RejectCredentialFields and NormalizeRegistrationConfig, called by both
// registration surfaces — the governed HTTP create path and the
// register_component agent tool — so the two cannot drift in what they accept.
//
// It refuses, wrapping ErrInvalidRegistrationConfig:
//
//   - a config that is present but not a JSON object;
//   - a field the type does not declare — which includes the adapter-read inline
//     secrets (credential.AdapterSecretFields), refused with their own message;
//   - a declared field whose value has the wrong JSON type, with no coercion: the
//     string "123" is not an integer;
//   - a url field whose value is not an absolute URL of the scheme://… form;
//   - a field that names another component (RefersTo) whose value breaks the
//     component-id format (D-0150; existence is deliberately not checked);
//   - a field required at registration that is absent.
//
// A field holding its type's zero value — "", 0 or false — counts as absent,
// which is how every adapter's ParseConfig treats it: an empty url is a missing
// url, and newrelic's account_id of 0 is a missing account. So an empty optional
// field is accepted and an empty required one is refused as missing.
//
// An absent or null config is a config with no fields: it registers when the
// type declares no registration-required field, and persists as {} (D-0057, as
// narrowed by D-0168).
//
// Nothing is resolved, dialed or probed: whether a URL answers is promotion's
// business. The function reads only the declaration and the submitted bytes.
func ValidateRegistrationConfig(componentType string, config json.RawMessage) error {
	fields, ok := credential.RegistrationFields(componentType)
	if !ok {
		// Every registrable type is declared (TestRegistrationContract_
		// CoversEveryRegistrableType); reaching this means a type was admitted
		// without a contract, so refuse rather than accept anything.
		return fmt.Errorf("%w: no registration contract is declared for type %q", ErrInvalidRegistrationConfig, componentType)
	}

	decoded := map[string]json.RawMessage{}
	if !isAbsentConfig(config) {
		if err := json.Unmarshal(config, &decoded); err != nil || decoded == nil {
			return fmt.Errorf("%w: config must be a JSON object", ErrInvalidRegistrationConfig)
		}
	}

	declared := make(map[string]struct{}, len(fields))
	names := make([]string, 0, len(fields))
	for _, f := range fields {
		declared[f.Name] = struct{}{}
		names = append(names, f.Name)
	}

	// Undeclared keys first, in a stable order so the error is deterministic.
	keys := make([]string, 0, len(decoded))
	for k := range decoded {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, ok := declared[k]; ok {
			continue
		}
		if credential.IsAdapterSecretField(k) {
			return fmt.Errorf("%w: %q is a secret, not a registration field — credentials are supplied only at promotion",
				ErrInvalidRegistrationConfig, k)
		}
		return fmt.Errorf("%w: %q is not a registration field for type %q (accepted: %s)",
			ErrInvalidRegistrationConfig, k, componentType, strings.Join(names, ", "))
	}

	for _, f := range fields {
		raw, present := decoded[f.Name]
		if present {
			empty, err := checkFieldValue(f, raw)
			if err != nil {
				return fmt.Errorf("%w: %q %s", ErrInvalidRegistrationConfig, f.Name, err.Error())
			}
			present = !empty
		}
		if !present && f.Phase == credential.PhaseRegistration {
			return fmt.Errorf("%w: %q is required for type %q (%s)",
				ErrInvalidRegistrationConfig, f.Name, componentType, f.Label)
		}
	}
	return nil
}

// isAbsentConfig reports whether a submitted config carries nothing: no bytes,
// or a JSON null.
func isAbsentConfig(config json.RawMessage) bool {
	trimmed := bytes.TrimSpace(config)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}

// checkFieldValue checks one present value against its declaration. It reports
// whether the value is its type's zero value (and so counts as absent), or why
// the value does not have the declared shape.
func checkFieldValue(f credential.RegistrationField, raw json.RawMessage) (empty bool, err error) {
	raw = bytes.TrimSpace(raw)
	switch f.Type {
	case credential.FieldString, credential.FieldURL:
		var s string
		if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &s) != nil {
			return false, errors.New("must be a JSON string")
		}
		if s == "" {
			return true, nil
		}
		if f.Type == credential.FieldURL && !IsAbsoluteURL(s) {
			return false, fmt.Errorf("must be an absolute URL such as https://host:port, got %q", s)
		}
		if len(f.RefersTo) > 0 {
			if err := ValidateComponentID(s); err != nil {
				return false, err
			}
		}
		return false, nil
	case credential.FieldInteger:
		n, perr := strconv.ParseInt(string(raw), 10, 64)
		if perr != nil {
			return false, errors.New("must be a JSON integer")
		}
		return n == 0, nil
	case credential.FieldBoolean:
		switch string(raw) {
		case "true":
			return false, nil
		case "false":
			return true, nil
		default:
			return false, errors.New("must be a JSON boolean")
		}
	default:
		// A declared type this switch does not know is a contract bug the
		// declaration test forbids; refuse rather than accept unchecked.
		return false, fmt.Errorf("has an undeclarable type %q", f.Type)
	}
}

// IsAbsoluteURL reports whether s parses as an absolute URL of the scheme://…
// form: a scheme, no opaque part, and a host or a path. It admits
// https://prometheus:9090 and file:///srv/repo, and refuses prometheus:9090 (a
// scheme with an opaque part — the missing "http://" mistake), a bare host, and
// anything url.Parse rejects. Shape only: nothing is resolved or dialed.
func IsAbsoluteURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return u.Scheme != "" && u.Opaque == "" && (u.Host != "" || u.Path != "")
}
