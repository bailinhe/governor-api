package v1alpha1

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	events "github.com/metal-toolbox/governor-api/pkg/events/v1alpha1"

	"github.com/cockroachdb/cockroach-go/v2/testserver"
	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
	"github.com/metal-toolbox/auditevent/ginaudit"
	dbm "github.com/metal-toolbox/governor-api/db"
	"github.com/metal-toolbox/governor-api/internal/eventbus"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"go.hollow.sh/toolbox/ginauth"
)

type mockNATSConn struct {
	Subject string
	Payload []byte
}

func (m *mockNATSConn) Drain() error { return nil }
func (m *mockNATSConn) Publish(s string, p []byte) error {
	m.Subject = s
	m.Payload = p

	return nil
}

type SystemExtensionResourceTestSuite struct {
	suite.Suite

	db   *sql.DB
	conn *mockNATSConn
}

func (s *SystemExtensionResourceTestSuite) seedTestDB() error {
	testData := []string{
		`INSERT INTO extensions (id, name, description, enabled, slug, status) 
		VALUES ('00000001-0000-0000-0000-000000000001', 'Test Extension', 'some extension', true, 'test-extension', 'online');`,
		`
		INSERT INTO extension_resource_definitions (id, name, description, enabled, slug_singular, slug_plural, version, scope, schema, extension_id) 
		VALUES ('00000001-0000-0000-0000-000000000002', 'Test Resource', 'some-description', true, 'test-resource', 'test-resources', 'v1', 'system',
		'{"$id": "v1.person.test-ex-1","$schema": "https://json-schema.org/draft/2020-12/schema","title": "Person","type": "object","unique": ["firstName", "lastName"],"required": ["firstName", "lastName"],"properties": {"firstName": {"type": "string","description": "The person''s first name.","ui": {"hide": true}},"lastName": {"type": "string","description": "The person''s last name."},"age": {"description": "Age in years which must be equal to or greater than zero.","type": "integer","minimum": 0}}}'::jsonb,
		'00000001-0000-0000-0000-000000000001');
		`,
		`
		INSERT INTO extension_resource_definitions (id, name, description, enabled, slug_singular, slug_plural, version, scope, schema, extension_id) 
		VALUES ('00000001-0000-0000-0000-000000000003', 'User Resource', 'some-description', true, 'user-resource', 'user-resources', 'v1', 'user',
		'{"$id": "v1.person.test-ex-1","$schema": "https://json-schema.org/draft/2020-12/schema","title": "Person","type": "object","unique": ["firstName", "lastName"],"required": ["firstName", "lastName"],"properties": {"firstName": {"type": "string","description": "The person''s first name.","ui": {"hide": true}},"lastName": {"type": "string","description": "The person''s last name."},"age": {"description": "Age in years which must be equal to or greater than zero.","type": "integer","minimum": 0}}}'::jsonb,
		'00000001-0000-0000-0000-000000000001');
		`,
		`INSERT INTO system_extension_resources (id, resource, extension_resource_definition_id)
		VALUES ( '00000001-0000-0000-0000-000000000004', '{"age": 10, "firstName": "Hello", "lastName": "World"}'::jsonb, '00000001-0000-0000-0000-000000000002');`,
		`INSERT INTO system_extension_resources (id, resource, extension_resource_definition_id, deleted_at)
		VALUES ( '00000001-0000-0000-0000-000000000005', '{"age": 10, "firstName": "Hello", "lastName": "World"}'::jsonb, '00000001-0000-0000-0000-000000000002', '2023-07-12 12:00:00.000000+00');`,
		`INSERT INTO system_extension_resources (id, resource, extension_resource_definition_id)
		VALUES ( '00000001-0000-0000-0000-000000000006', '{"age": 30, "firstName": "Hello1", "lastName": "World1"}'::jsonb, '00000001-0000-0000-0000-000000000002');`,
		`INSERT INTO system_extension_resources (id, resource, extension_resource_definition_id)
		VALUES ( '00000001-0000-0000-0000-000000000007', '{"age": 30, "firstName": "Hello2", "lastName": "World2"}'::jsonb, '00000001-0000-0000-0000-000000000002');`,
		`
		INSERT INTO extension_resource_definitions (id, name, description, enabled, slug_singular, slug_plural, version, scope, schema, extension_id) 
		VALUES ('00000001-0000-0000-0000-000000000008', 'Test Resource No Constraint', 'some-description', true, 'test-resource-no-constraint', 'test-resources-no-constraint', 'v1', 'system',
		'{"$id": "v1.person.test-ex-1","$schema": "https://json-schema.org/draft/2020-12/schema","title": "Person","type": "object","required": ["firstName", "lastName"],"properties": {"firstName": {"type": "string","description": "The person''s first name.","ui": {"hide": true}},"lastName": {"type": "string","description": "The person''s last name."},"age": {"description": "Age in years which must be equal to or greater than zero.","type": "integer","minimum": 0}}}'::jsonb,
		'00000001-0000-0000-0000-000000000001');
		`,
		`INSERT INTO system_extension_resources (id, resource, extension_resource_definition_id)
		VALUES ( '00000001-0000-0000-0000-000000000009', '{"age": 10, "firstName": "Hello", "lastName": "World"}'::jsonb, '00000001-0000-0000-0000-000000000008');`,
	}

	for _, q := range testData {
		_, err := s.db.Query(q)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *SystemExtensionResourceTestSuite) runTestHTTPServer() *gin.Engine {
	s.conn = &mockNATSConn{}
	router := gin.New()
	v1alphaRtr := Router{
		AdminGroups: []string{"governor-admin"},
		AuthMW:      &ginauth.MultiTokenMiddleware{},
		AuditMW:     ginaudit.NewJSONMiddleware("governor-api", io.Discard),
		DB:          sqlx.NewDb(s.db, "postgres"),
		EventBus:    eventbus.NewClient(eventbus.WithNATSConn(s.conn)),
	}
	v1alpha1 := router.Group("/api/v1alpha1")
	v1alphaRtr.Routes(v1alpha1)

	return router
}

func (s *SystemExtensionResourceTestSuite) SetupSuite() {
	ts, err := testserver.NewTestServer()
	if err != nil {
		panic(err)
	}

	s.db, err = sql.Open("postgres", ts.PGURL().String())
	if err != nil {
		panic(err)
	}

	goose.SetBaseFS(dbm.Migrations)

	if err := goose.Up(s.db, "migrations"); err != nil {
		panic("migration failed - could not set up test db")
	}

	if err := s.seedTestDB(); err != nil {
		panic("db setup failed - could not seed test db: " + err.Error())
	}
}

func (s *SystemExtensionResourceTestSuite) TestFindERDForExtensionResource() {
	tests := []struct {
		name          string
		extensionSlug string
		erdSlugPlural string
		erdVersion    string
		expectedErr   error
		expectedERDID string
	}{
		{
			name:          "valid lookup",
			extensionSlug: "test-extension",
			erdSlugPlural: "test-resources",
			erdVersion:    "v1",
			expectedErr:   nil,
			expectedERDID: "00000001-0000-0000-0000-000000000002",
		},
		{
			name:          "invalid extension slug",
			extensionSlug: "invalid-slug",
			erdSlugPlural: "test-resources",
			erdVersion:    "v1",
			expectedErr:   ErrExtensionNotFound,
		},
		{
			name:          "invalid ERD SlugPlural",
			extensionSlug: "test-extension",
			erdSlugPlural: "invalid-slug",
			erdVersion:    "v1",
			expectedErr:   ErrERDNotFound,
		},
		{
			name:          "invalid ERD version",
			extensionSlug: "test-extension",
			erdSlugPlural: "test-resources",
			erdVersion:    "v2",
			expectedErr:   ErrERDNotFound,
		},
	}

	for _, tt := range tests {
		s.T().Run(tt.name, func(t *testing.T) {
			ext, erd, err := findERDForExtensionResource(context.Background(), s.db, tt.extensionSlug, tt.erdSlugPlural, tt.erdVersion)

			if tt.expectedErr != nil {
				assert.NotNil(t, err)
				assert.ErrorIs(
					t, err, tt.expectedErr,
					"Expected error %v, got %v", tt.expectedErr, err,
				)

				return
			}

			assert.Equal(
				t, ext.Slug, tt.extensionSlug,
				"Expected extension slug %s, got %s", tt.extensionSlug, ext.Slug,
			)

			assert.Equal(
				t, erd.ID, tt.expectedERDID,
				"Expected ERD ID %s, got %s", tt.expectedERDID, erd.ID,
			)
		})
	}
}

func (s *SystemExtensionResourceTestSuite) TestListSystemExtensionResources() {
	r := s.runTestHTTPServer()

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectedErrMsg string
		expectedCount  int
	}{
		{
			name:           "ok",
			url:            "/api/v1alpha1/extension-resources/test-extension/test-resources/v1",
			expectedStatus: http.StatusOK,
			expectedCount:  3,
		},
		{
			name:           "extension not found",
			url:            "/api/v1alpha1/extension-resources/nonexistent-extension/test-resources/v1",
			expectedStatus: http.StatusNotFound,
			expectedErrMsg: "extension does not exist",
		},
		{
			name:           "ERD not found",
			url:            "/api/v1alpha1/extension-resources/test-extension/nonexistent-resources/v1",
			expectedStatus: http.StatusNotFound,
			expectedErrMsg: "ERD does not exist",
		},
		{
			name:           "incorrect ERD scope",
			url:            "/api/v1alpha1/extension-resources/test-extension/user-resources/v1",
			expectedStatus: http.StatusBadRequest,
			expectedErrMsg: "cannot list system resources for user scoped user-resource/v1",
		},
		{
			name:           "integer URI queries",
			url:            "/api/v1alpha1/extension-resources/test-extension/test-resources/v1?age=10",
			expectedStatus: http.StatusOK,
			expectedCount:  1,
		},
		{
			name:           "list deleted",
			url:            "/api/v1alpha1/extension-resources/test-extension/test-resources/v1?deleted",
			expectedStatus: http.StatusOK,
			expectedCount:  5,
		},
		{
			name:           "integer URI queries w/ results",
			url:            "/api/v1alpha1/extension-resources/test-extension/test-resources/v1?age=10",
			expectedStatus: http.StatusOK,
			expectedCount:  1,
		},
		{
			name:           "integer URI queries w/o results",
			url:            "/api/v1alpha1/extension-resources/test-extension/test-resources/v1?age=11",
			expectedStatus: http.StatusOK,
			expectedCount:  0,
		},
		{
			name:           "string URI queries w results",
			url:            `/api/v1alpha1/extension-resources/test-extension/test-resources/v1?firstName="Hello"`,
			expectedStatus: http.StatusOK,
			expectedCount:  1,
		},
		{
			name:           "string URI queries w/o results",
			url:            `/api/v1alpha1/extension-resources/test-extension/test-resources/v1?firstName="World"`,
			expectedStatus: http.StatusOK,
			expectedCount:  0,
		},
	}

	for _, tt := range tests {
		s.T().Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", tt.url, nil)
			req = req.WithContext(context.Background())
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "Expected status %d, got %d", tt.expectedStatus, w.Code)

			if tt.expectedErrMsg != "" {
				body := w.Body.String()
				assert.Contains(
					t, body, tt.expectedErrMsg,
					"Expected error message to contain %q, got %s", tt.expectedErrMsg, body,
				)

				return
			}

			body := w.Body.String()
			resp := []interface{}{}
			err := json.Unmarshal([]byte(body), &resp)

			assert.Nil(t, err, "expecting unmarshal err to be nil")
			assert.Equal(t, tt.expectedCount, len(resp))
		})
	}
}

func (s *SystemExtensionResourceTestSuite) TestGetSystemExtensionResource() {
	r := s.runTestHTTPServer()

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectedErrMsg string
		expectedCount  int
	}{
		{
			name:           "ok",
			url:            "/api/v1alpha1/extension-resources/test-extension/test-resources/v1/00000001-0000-0000-0000-000000000004",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "get deleted ok",
			url:            "/api/v1alpha1/extension-resources/test-extension/test-resources/v1/00000001-0000-0000-0000-000000000005?deleted",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "extension not found",
			url:            "/api/v1alpha1/extension-resources/nonexistent-extension/test-resources/v1/00000001-0000-0000-0000-000000000004",
			expectedStatus: http.StatusNotFound,
			expectedErrMsg: "extension does not exist",
		},
		{
			name:           "ERD not found",
			url:            "/api/v1alpha1/extension-resources/test-extension/nonexistent-resources/v1/00000001-0000-0000-0000-000000000004",
			expectedStatus: http.StatusNotFound,
			expectedErrMsg: "ERD does not exist",
		},
		{
			name:           "incorrect ERD scope",
			url:            "/api/v1alpha1/extension-resources/test-extension/user-resources/v1/00000001-0000-0000-0000-000000000004",
			expectedStatus: http.StatusBadRequest,
			expectedErrMsg: "cannot get system resource for user scoped user-resource/v1",
		},
	}

	for _, tt := range tests {
		s.T().Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", tt.url, nil)
			req = req.WithContext(context.Background())
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "Expected status %d, got %d", tt.expectedStatus, w.Code)

			if tt.expectedErrMsg != "" {
				body := w.Body.String()
				assert.Contains(
					t, body, tt.expectedErrMsg,
					"Expected error message to contain %q, got %s", tt.expectedErrMsg, body,
				)

				return
			}
		})
	}
}

func (s *SystemExtensionResourceTestSuite) TestCreateSystemExtensionResource() {
	r := s.runTestHTTPServer()

	tests := []struct {
		name                 string
		url                  string
		payload              string
		expectedStatus       int
		expectedErrMsg       string
		expectedEventSubject string
		expectedEventPayload *events.Event
	}{
		{
			name:                 "ok",
			url:                  "/api/v1alpha1/extension-resources/test-extension/test-resources/v1",
			expectedStatus:       http.StatusCreated,
			payload:              `{ "age": 100, "firstName": "test", "lastName": "1" }`,
			expectedEventSubject: "events.test-resources",
			expectedEventPayload: &events.Event{
				Version:                       "v1",
				Action:                        events.GovernorEventCreate,
				ExtensionID:                   "00000001-0000-0000-0000-000000000001",
				ExtensionResourceDefinitionID: "00000001-0000-0000-0000-000000000002",
			},
		},
		{
			name:                 "duplicate entry with no constraint",
			url:                  "/api/v1alpha1/extension-resources/test-extension/test-resources-no-constraint/v1",
			expectedStatus:       http.StatusCreated,
			payload:              `{"age": 10, "firstName": "Hello", "lastName": "World"}`,
			expectedEventSubject: "events.test-resources-no-constraint",
			expectedEventPayload: &events.Event{
				Version:                       "v1",
				Action:                        events.GovernorEventCreate,
				ExtensionID:                   "00000001-0000-0000-0000-000000000001",
				ExtensionResourceDefinitionID: "00000001-0000-0000-0000-000000000008",
			},
		},
		{
			name:           "create violates unique constraint",
			url:            "/api/v1alpha1/extension-resources/test-extension/test-resources/v1",
			expectedStatus: http.StatusBadRequest,
			expectedErrMsg: "unique constraint violation",
			payload:        `{ "age": 10, "firstName": "test", "lastName": "1" }`,
		},
		{
			name:           "json schema validation failed",
			url:            "/api/v1alpha1/extension-resources/test-extension/test-resources/v1",
			expectedStatus: http.StatusBadRequest,
			expectedErrMsg: "'/age' does not validate with",
			payload:        `{ "age": -1, "firstName": "test", "lastName": "2" }`,
		},
		{
			name:           "extension not found",
			url:            "/api/v1alpha1/extension-resources/nonexistent-extension/test-resources/v1",
			expectedStatus: http.StatusNotFound,
			expectedErrMsg: "extension does not exist",
		},
		{
			name:           "ERD not found",
			url:            "/api/v1alpha1/extension-resources/test-extension/nonexistent-resources/v1",
			expectedStatus: http.StatusNotFound,
			expectedErrMsg: "ERD does not exist",
		},
		{
			name:           "incorrect ERD scope",
			url:            "/api/v1alpha1/extension-resources/test-extension/user-resources/v1",
			expectedStatus: http.StatusBadRequest,
			expectedErrMsg: "cannot create system resource for user scoped user-resource/v1",
			payload:        `{ "age": 10, "firstName": "test", "lastName": "1" }`,
		},
	}

	for _, tt := range tests {
		s.T().Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("POST", tt.url, strings.NewReader(tt.payload))
			req = req.WithContext(context.Background())
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "Expected status %d, got %d", tt.expectedStatus, w.Code)

			if tt.expectedErrMsg != "" {
				body := w.Body.String()
				assert.Contains(
					t, body, tt.expectedErrMsg,
					"Expected error message to contain %q, got %s", tt.expectedErrMsg, body,
				)

				return
			}

			assert.Equal(
				t, tt.expectedEventSubject, s.conn.Subject,
				"Expected event subject %s, got %s", tt.expectedEventSubject, s.conn.Subject,
			)

			event := &events.Event{}
			err := json.Unmarshal(s.conn.Payload, event)
			assert.Nil(t, err)

			sr := &SystemExtensionResource{}
			body := w.Body.String()
			err = json.Unmarshal([]byte(body), sr)
			assert.Nil(t, err)

			assert.Equal(
				t, tt.expectedEventPayload.Version, event.Version,
				"Expected event version %s, got %s", tt.expectedEventPayload.Version, event.Version,
			)

			assert.Equal(
				t, tt.expectedEventPayload.Action, event.Action,
				"Expected event action %s, got %s", tt.expectedEventPayload.Action, event.Action,
			)

			assert.Equal(
				t, tt.expectedEventPayload.ExtensionID, event.ExtensionID,
				"Expected event extension ID %s, got %s", tt.expectedEventPayload.ExtensionID, event.ExtensionID,
			)

			assert.Equal(
				t, tt.expectedEventPayload.ExtensionResourceDefinitionID, event.ExtensionResourceDefinitionID,
				"Expected event ERD ID %s, got %s", tt.expectedEventPayload.ExtensionResourceDefinitionID, event.ExtensionResourceDefinitionID,
			)

			assert.Equal(
				t, event.ExtensionResourceID, sr.ID,
				"Expected event ERD ID to match response ID",
			)
		})
	}
}

func (s *SystemExtensionResourceTestSuite) TestUpdateSystemExtensionResource() {
	r := s.runTestHTTPServer()

	tests := []struct {
		name                 string
		url                  string
		payload              string
		expectedStatus       int
		expectedErrMsg       string
		expectedEventSubject string
		expectedEventPayload *events.Event
	}{
		{
			name:                 "ok",
			url:                  "/api/v1alpha1/extension-resources/test-extension/test-resources/v1/00000001-0000-0000-0000-000000000004",
			expectedStatus:       http.StatusAccepted,
			payload:              `{ "age": 10, "firstName": "Hello", "lastName": "World" }`,
			expectedEventSubject: "events.test-resources",
			expectedEventPayload: &events.Event{
				Version:                       "v1",
				Action:                        events.GovernorEventUpdate,
				ExtensionID:                   "00000001-0000-0000-0000-000000000001",
				ExtensionResourceDefinitionID: "00000001-0000-0000-0000-000000000002",
			},
		},
		{
			name:           "update violates unique constraint",
			url:            "/api/v1alpha1/extension-resources/test-extension/test-resources/v1/00000001-0000-0000-0000-000000000004",
			expectedStatus: http.StatusBadRequest,
			expectedErrMsg: "unique constraint violation",
			payload:        `{ "age": 10, "firstName": "Hello1", "lastName": "World1" }`,
		},
		{
			name:           "json schema validation failed",
			url:            "/api/v1alpha1/extension-resources/test-extension/test-resources/v1/00000001-0000-0000-0000-000000000004",
			expectedStatus: http.StatusBadRequest,
			expectedErrMsg: "'/age' does not validate with",
			payload:        `{ "age": -1, "firstName": "test", "lastName": "2" }`,
		},
		{
			name:           "extension not found",
			url:            "/api/v1alpha1/extension-resources/nonexistent-extension/test-resources/v1/00000001-0000-0000-0000-000000000004",
			expectedStatus: http.StatusNotFound,
			expectedErrMsg: "extension does not exist",
		},
		{
			name:           "ERD not found",
			url:            "/api/v1alpha1/extension-resources/test-extension/nonexistent-resources/v1/00000001-0000-0000-0000-000000000004",
			expectedStatus: http.StatusNotFound,
			expectedErrMsg: "ERD does not exist",
		},
		{
			name:           "incorrect ERD scope",
			url:            "/api/v1alpha1/extension-resources/test-extension/user-resources/v1/00000001-0000-0000-0000-000000000005",
			expectedStatus: http.StatusBadRequest,
			expectedErrMsg: "cannot update system resource for user scoped user-resource/v1",
			payload:        `{ "age": 10, "firstName": "test", "lastName": "1" }`,
		},
	}

	for _, tt := range tests {
		s.T().Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("PATCH", tt.url, strings.NewReader(tt.payload))
			req = req.WithContext(context.Background())
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "Expected status %d, got %d", tt.expectedStatus, w.Code)

			if tt.expectedErrMsg != "" {
				body := w.Body.String()
				assert.Contains(
					t, body, tt.expectedErrMsg,
					"Expected error message to contain %q, got %s", tt.expectedErrMsg, body,
				)

				return
			}

			assert.Equal(
				t, tt.expectedEventSubject, s.conn.Subject,
				"Expected event subject %s, got %s", tt.expectedEventSubject, s.conn.Subject,
			)

			event := &events.Event{}
			err := json.Unmarshal(s.conn.Payload, event)
			assert.Nil(t, err)

			sr := &SystemExtensionResource{}
			body := w.Body.String()
			err = json.Unmarshal([]byte(body), sr)
			assert.Nil(t, err)

			assert.Equal(
				t, tt.expectedEventPayload.Version, event.Version,
				"Expected event version %s, got %s", tt.expectedEventPayload.Version, event.Version,
			)

			assert.Equal(
				t, tt.expectedEventPayload.Action, event.Action,
				"Expected event action %s, got %s", tt.expectedEventPayload.Action, event.Action,
			)

			assert.Equal(
				t, tt.expectedEventPayload.ExtensionID, event.ExtensionID,
				"Expected event extension ID %s, got %s", tt.expectedEventPayload.ExtensionID, event.ExtensionID,
			)

			assert.Equal(
				t, tt.expectedEventPayload.ExtensionResourceDefinitionID, event.ExtensionResourceDefinitionID,
				"Expected event ERD ID %s, got %s", tt.expectedEventPayload.ExtensionResourceDefinitionID, event.ExtensionResourceDefinitionID,
			)

			assert.Equal(
				t, event.ExtensionResourceID, sr.ID,
				"Expected event ERD ID to match response ID",
			)
		})
	}
}

func (s *SystemExtensionResourceTestSuite) TestDeleteSystemExtensionResource() {
	r := s.runTestHTTPServer()

	tests := []struct {
		name                 string
		url                  string
		expectedStatus       int
		expectedErrMsg       string
		expectedEventSubject string
		expectedEventPayload *events.Event
	}{
		{
			name:                 "ok",
			url:                  "/api/v1alpha1/extension-resources/test-extension/test-resources/v1/00000001-0000-0000-0000-000000000007",
			expectedStatus:       http.StatusAccepted,
			expectedEventSubject: "events.test-resources",
			expectedEventPayload: &events.Event{
				Version:                       "v1",
				Action:                        events.GovernorEventDelete,
				ExtensionID:                   "00000001-0000-0000-0000-000000000001",
				ExtensionResourceDefinitionID: "00000001-0000-0000-0000-000000000002",
			},
		},
		{
			name:           "extension not found",
			url:            "/api/v1alpha1/extension-resources/nonexistent-extension/test-resources/v1/00000001-0000-0000-0000-000000000007",
			expectedStatus: http.StatusNotFound,
			expectedErrMsg: "extension does not exist",
		},
		{
			name:           "ERD not found",
			url:            "/api/v1alpha1/extension-resources/test-extension/nonexistent-resources/v1/00000001-0000-0000-0000-000000000007",
			expectedStatus: http.StatusNotFound,
			expectedErrMsg: "ERD does not exist",
		},
		{
			name:           "resource not found",
			url:            "/api/v1alpha1/extension-resources/test-extension/test-resources/v1/00000001-0000-0000-0000-000000000001",
			expectedStatus: http.StatusNotFound,
			expectedErrMsg: "resource not found",
		},
		{
			name:           "incorrect ERD scope",
			url:            "/api/v1alpha1/extension-resources/test-extension/user-resources/v1/00000001-0000-0000-0000-000000000007",
			expectedStatus: http.StatusBadRequest,
			expectedErrMsg: "cannot delete system resource for user scoped user-resource/v1",
		},
	}

	for _, tt := range tests {
		s.T().Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("DELETE", tt.url, nil)
			req = req.WithContext(context.Background())
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "Expected status %d, got %d", tt.expectedStatus, w.Code)

			if tt.expectedErrMsg != "" {
				body := w.Body.String()
				assert.Contains(
					t, body, tt.expectedErrMsg,
					"Expected error message to contain %q, got %s", tt.expectedErrMsg, body,
				)

				return
			}

			assert.Equal(
				t, tt.expectedEventSubject, s.conn.Subject,
				"Expected event subject %s, got %s", tt.expectedEventSubject, s.conn.Subject,
			)

			event := &events.Event{}
			err := json.Unmarshal(s.conn.Payload, event)
			assert.Nil(t, err)

			sr := &SystemExtensionResource{}
			body := w.Body.String()
			err = json.Unmarshal([]byte(body), sr)
			assert.Nil(t, err)

			assert.Equal(
				t, tt.expectedEventPayload.Version, event.Version,
				"Expected event version %s, got %s", tt.expectedEventPayload.Version, event.Version,
			)

			assert.Equal(
				t, tt.expectedEventPayload.Action, event.Action,
				"Expected event action %s, got %s", tt.expectedEventPayload.Action, event.Action,
			)

			assert.Equal(
				t, tt.expectedEventPayload.ExtensionID, event.ExtensionID,
				"Expected event extension ID %s, got %s", tt.expectedEventPayload.ExtensionID, event.ExtensionID,
			)

			assert.Equal(
				t, tt.expectedEventPayload.ExtensionResourceDefinitionID, event.ExtensionResourceDefinitionID,
				"Expected event ERD ID %s, got %s", tt.expectedEventPayload.ExtensionResourceDefinitionID, event.ExtensionResourceDefinitionID,
			)

			assert.Equal(
				t, event.ExtensionResourceID, sr.ID,
				"Expected event ERD ID to match response ID",
			)
		})
	}
}

func TestSystemExtensionResourceSuite(t *testing.T) {
	suite.Run(t, new(SystemExtensionResourceTestSuite))
}
