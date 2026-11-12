package migration_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"go.yaml.in/yaml/v3"
)

// These guard the handoff artifacts, not unimplemented M2 runtime behavior.
// Standards/schema validation is separately performed by the Redocly CLI.
func contractRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate contract source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func contractFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(contractRoot(t), "api", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func contractDocument(t *testing.T) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := yaml.Unmarshal(contractFile(t, "openapi.yaml"), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func object(t *testing.T, value any) map[string]any {
	t.Helper()
	got, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected object, got %T", value)
	}
	return got
}

func resolve(t *testing.T, doc map[string]any, reference string) map[string]any {
	t.Helper()
	if !strings.HasPrefix(reference, "#/") {
		t.Fatalf("contract must be self-contained, got %q", reference)
	}
	var value any = doc
	for _, part := range strings.Split(reference[2:], "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		var ok bool
		value, ok = object(t, value)[part]
		if !ok {
			t.Fatalf("unresolved reference %q", reference)
		}
	}
	return object(t, value)
}

func walkContract(value any, visit func(map[string]any)) {
	switch value := value.(type) {
	case map[string]any:
		visit(value)
		for _, child := range value {
			walkContract(child, visit)
		}
	case []any:
		for _, child := range value {
			walkContract(child, visit)
		}
	}
}

func TestM2ContractReferencesAndSecurity(t *testing.T) {
	doc := contractDocument(t)
	if doc["openapi"] != "3.0.3" {
		t.Fatal("review tooling before changing OpenAPI version")
	}
	walkContract(doc, func(node map[string]any) {
		if reference, ok := node["$ref"].(string); ok {
			resolve(t, doc, reference)
			if len(node) != 1 {
				t.Errorf("OpenAPI 3.0 reference siblings are ignored: %s", reference)
			}
		}
	})
	components := object(t, doc["components"])
	cookie := object(t, object(t, components["securitySchemes"])["sessionCookie"])
	if cookie["name"] != security.SessionCookieName || cookie["in"] != "cookie" {
		t.Fatal("K1 cookie scheme differs from M1")
	}
	csrf := object(t, object(t, components["parameters"])["csrf"])
	if csrf["name"] != security.CSRFHeaderName || csrf["required"] != true {
		t.Fatal("K1 CSRF header differs from M1")
	}
	ids := map[string]bool{}
	pathParam := regexp.MustCompile(`\{([^}]+)\}`)
	for path, raw := range object(t, doc["paths"]) {
		item := object(t, raw)
		for _, match := range pathParam.FindAllStringSubmatch(path, -1) {
			found := false
			for _, p := range item["parameters"].([]any) {
				p := resolve(t, doc, object(t, p)["$ref"].(string))
				found = found || p["name"] == match[1] && p["in"] == "path" && p["required"] == true
			}
			if !found {
				t.Errorf("missing required path parameter %s in %s", match[1], path)
			}
		}
		for method, raw := range item {
			if !slices.Contains([]string{"get", "post", "put", "patch", "delete"}, method) {
				continue
			}
			op := object(t, raw)
			id, _ := op["operationId"].(string)
			if id == "" || ids[id] || op["x-access"] == nil {
				t.Errorf("missing/duplicate operationId or access policy: %s %s", method, path)
			}
			ids[id] = true
			if path == "/healthz" || path == "/metrics" {
				if item["servers"] == nil || op["security"] == nil {
					t.Errorf("root operations endpoint must override prefix/security: %s", path)
				}
				continue
			}
			responses := object(t, op["responses"])
			for _, code := range []string{"401", "403", "422", "500", "503"} {
				if responses[code] == nil {
					t.Errorf("missing %s response in %s", code, id)
				}
			}
			if method != "get" && path != "/auth/login" {
				found := false
				for _, p := range op["parameters"].([]any) {
					found = found || object(t, p)["$ref"] == "#/components/parameters/csrf"
				}
				if !found {
					t.Errorf("unsafe authenticated operation lacks CSRF: %s", id)
				}
			}
		}
	}
	for _, id := range []string{
		"login", "logout", "getCurrentUser", "listUsers", "createUser", "updateUser",
		"listGroups", "createGroup", "updateGroup", "deleteGroup", "listGroupMembers", "addGroupMember", "removeGroupMember", "setGroupLeader",
		"listConnections", "createConnection", "deleteConnection", "listGrants", "setGrant", "revokeGrant",
		"listDatabases", "getSchema", "getTable", "listRows", "compareSchemas", "listMigrations", "createMigration", "getMigration", "updateMigration", "explainMigration",
		"submitChangeRequest", "listChangeRequests", "getChangeRequest", "decideChangeRequest", "enqueueChangeRequest",
		"getJob", "listJobAttempts", "listDatabaseJobs", "listExecutions", "getExecution", "listExecutedStatements", "getPosition", "health", "metrics",
	} {
		if !ids[id] {
			t.Errorf("missing K1 operation %s", id)
		}
	}
}

func enumStrings[T ~string](values ...T) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = string(value)
	}
	return result
}

func TestM2ContractEnumParityAndLimits(t *testing.T) {
	doc := contractDocument(t)
	schemas := object(t, object(t, doc["components"])["schemas"])
	for name, expected := range map[string][]string{
		"Access":          enumStrings(security.AccessViewOnly, security.AccessViewEdit),
		"ExecutionMode":   enumStrings(migration.ModeNormal, migration.ModeBatch),
		"DefinitionState": enumStrings(migration.DefinitionDraft, migration.DefinitionFrozen),
		"ChangeKind":      enumStrings(migration.ChangeApply, migration.ChangeRollback, migration.ChangeForward, migration.ChangeResume, migration.ChangeRevertBatches),
		"CRStatus":        enumStrings(migration.CRPendingReview, migration.CRApproved, migration.CRRejected, migration.CRQueued, migration.CRRunning, migration.CRSucceeded, migration.CRFailed, migration.CRPartial, migration.CRUncertain, migration.CRPartiallyReverted),
		"JobStatus":       enumStrings(migration.JobQueued, migration.JobRunning, migration.JobSucceeded, migration.JobFailed, migration.JobPartial, migration.JobUncertain, migration.JobPartiallyReverted),
		"Direction":       enumStrings(migration.DirectionUp, migration.DirectionDown),
		"Decision":        enumStrings(migration.DecisionApprove, migration.DecisionReject),
		"JobErrorCode":    enumStrings(migration.JobErrorSQL, migration.JobErrorConnection, migration.JobErrorInterrupted, migration.JobErrorRevalidation, migration.JobErrorUncertain, migration.JobErrorUnsafeRevert),
	} {
		var got []string
		for _, value := range object(t, schemas[name])["enum"].([]any) {
			got = append(got, value.(string))
		}
		slices.Sort(got)
		slices.Sort(expected)
		if !slices.Equal(got, expected) {
			t.Errorf("%s: K1 %v != K2 %v", name, got, expected)
		}
	}
	props := object(t, object(t, schemas["BatchConfig"])["properties"])
	batchSize := object(t, props["batch_size"])
	if batchSize["minimum"] != migration.MinBatchSize || batchSize["maximum"] != migration.MaxBatchSize {
		t.Fatal("batch bounds differ between K1 and K2")
	}
	jobProps := object(t, object(t, schemas["Job"])["properties"])
	if object(t, jobProps["max_attempts"])["maximum"] != migration.MaxAttemptsLimit {
		t.Fatal("retry bound differs between K1 and K2")
	}
	for _, summary := range []string{"MigrationSummary", "ExecutionSummary"} {
		props := object(t, object(t, schemas[summary])["properties"])
		for _, field := range []string{"up_sql", "down_sql", "sql_snapshot", "statements"} {
			if props[field] != nil {
				t.Errorf("summary %s must not load %s for every list item", summary, field)
			}
		}
	}
	paths := object(t, doc["paths"])
	op := object(t, object(t, paths["/databases/{database_id}/change-requests/{change_request_id}/execute"])["post"])
	if object(t, op["responses"])["202"] == nil || object(t, op["responses"])["200"] != nil {
		t.Fatal("enqueue must return 202, not synchronous execution result")
	}
	if object(t, object(t, paths["/databases/{database_id}/change-requests/{change_request_id}/approvals"])["post"])["x-access"] != "required_group_leader" {
		t.Fatal("approval must not require an editor grant")
	}
	if object(t, object(t, paths["/jobs/{job_id}"])["get"])["x-access"] != "view_job_database" {
		t.Fatal("viewer must be able to read jobs")
	}
}

func fixtureJSON(t *testing.T, name string, value any) {
	t.Helper()
	if err := json.Unmarshal(contractFile(t, filepath.Join("examples", name)), value); err != nil {
		t.Fatal(err)
	}
}

func TestM2ContractFixtureExamplesAndSecretBoundaries(t *testing.T) {
	schemas := object(t, object(t, contractDocument(t)["components"])["schemas"])
	for file, name := range map[string]string{
		"migration-normal.json": "Migration", "job-queued.json": "Job",
		"change-request-pending.json": "ChangeRequest", "error-conflict.json": "ErrorEnvelope",
	} {
		var fixture, example any
		fixtureJSON(t, file, &fixture)
		data, err := json.Marshal(object(t, schemas[name])["example"])
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &example); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(fixture, example) {
			t.Errorf("%s differs from embedded %s example", file, name)
		}
	}
	for _, name := range []string{"Job", "JobAttempt", "ChangeRequest", "Migration", "AuthMe", "User", "Connection"} {
		props := object(t, object(t, schemas[name])["properties"])
		for _, forbidden := range []string{"password", "dsn", "session_id", "session_token", "trace_context", "lease_owner", "lease_until"} {
			if props[forbidden] != nil {
				t.Errorf("secret/internal field %s exposed in %s", forbidden, name)
			}
		}
	}
	for _, input := range []string{"DefinitionInput", "UpdateDefinitionInput", "SubmitChangeInput", "ApprovalInput", "ExecuteInput"} {
		if object(t, schemas[input])["additionalProperties"] != false {
			t.Errorf("%s must reject unknown input fields", input)
		}
	}
}

func TestM2ContractChecksumVector(t *testing.T) {
	var vector struct {
		CanonicalJSON string `json:"canonical_json"`
		SHA256        string `json:"sha256"`
	}
	fixtureJSON(t, "checksum-v1.json", &vector)
	if !json.Valid([]byte(vector.CanonicalJSON)) || strings.TrimSpace(vector.CanonicalJSON) != vector.CanonicalJSON {
		t.Fatal("checksum vector must be JSON without surrounding whitespace")
	}
	got := sha256.Sum256([]byte(vector.CanonicalJSON))
	if hex.EncodeToString(got[:]) != vector.SHA256 {
		t.Fatal("checksum fixture digest does not match exact UTF-8 bytes")
	}
	var canonical, definition map[string]any
	if err := json.Unmarshal([]byte(vector.CanonicalJSON), &canonical); err != nil {
		t.Fatal(err)
	}
	fixtureJSON(t, "migration-normal.json", &definition)
	if definition["checksum"] != vector.SHA256 || canonical["checksum_version"] != float64(1) {
		t.Fatal("definition/checksum version mismatch")
	}
	delete(canonical, "checksum_version")
	for key, value := range canonical {
		if !reflect.DeepEqual(value, definition[key]) {
			t.Errorf("checksum payload differs from reviewed definition: %s", key)
		}
	}
}

func TestM2ContractHistoryGoldenFixture(t *testing.T) {
	type execution struct {
		ID        security.ID `json:"id"`
		Migration security.ID `json:"migration_id"`
		Operation string      `json:"operation"`
		Reverts   security.ID `json:"reverts_execution_id"`
	}
	var fixture struct {
		Labels         map[security.ID]string      `json:"labels"`
		Full           struct{ Items []execution } `json:"full"`
		Hidden         struct{ Items []execution } `json:"hidden"`
		ExpectedHidden []security.ID               `json:"expected_hidden_execution_ids"`
	}
	fixtureJSON(t, "history-hide-rollback.json", &fixture)
	if len(fixture.Full.Items) != 5 || len(fixture.Hidden.Items) != 3 {
		t.Fatal("fixture must represent the complete E1-E5 PDF example")
	}
	hidden := map[security.ID]bool{}
	for _, e := range fixture.Full.Items {
		if !security.ValidID(e.ID) || !security.ValidID(e.Migration) {
			t.Fatal("invalid fixture identity")
		}
		if e.Reverts != "" {
			hidden[e.ID], hidden[e.Reverts] = true, true
		}
	}
	if len(hidden) != len(fixture.ExpectedHidden) {
		t.Fatal("fixture hides an unexpected number of executions")
	}
	for _, id := range fixture.ExpectedHidden {
		if !hidden[id] || fixture.Labels[id] != "E2" && fixture.Labels[id] != "E3" {
			t.Fatal("fixture must hide exactly E2/E3 by execution identity")
		}
	}
	var expected []execution
	for _, e := range fixture.Full.Items {
		if !hidden[e.ID] {
			expected = append(expected, e)
		}
	}
	if !reflect.DeepEqual(expected, fixture.Hidden.Items) {
		t.Fatal("filtered fixture changed order or hid the later forward execution")
	}
}
