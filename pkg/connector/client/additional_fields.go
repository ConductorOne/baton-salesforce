package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/conductorone/simpleforce"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

const (
	// DescribePathFmt is the SObject describe endpoint, used to check that a
	// configured field API name actually exists on an object before we put it in
	// a SELECT. Formatted with the SObject name.
	DescribePathFmt = "/services/data/v64.0/sobjects/%s/describe"

	// maxAdditionalFields caps how many extra fields config can append to a
	// SELECT. SOQL has a 100k character query limit; this keeps a misconfigured
	// list from getting anywhere near it.
	maxAdditionalFields = 50

	// maxDescribeAttempts is how many times we'll try to describe an object
	// before giving up and using the configured field names as-is. A single
	// flaky request shouldn't cost us canonical-casing resolution for the rest
	// of the client's life, but retrying forever would mean one describe call
	// per page of a sync against a describe endpoint that is genuinely broken.
	maxDescribeAttempts = 3
)

// additionalFieldNamePattern matches a bare Salesforce field API name: a letter
// followed by letters, digits, and underscores (custom fields end in "__c",
// namespaced ones look like "acme__Role__c").
//
// Configured names are interpolated into the SELECT clause verbatim — SOQL has
// no bind parameters for field names — so this pattern is also the injection
// guard. Anything it does not match never reaches a query. Relationship
// traversals ("Manager.Name") are deliberately excluded: they can't be checked
// against a single object's describe, and they're out of scope for this option.
var additionalFieldNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,254}$`)

// NormalizeAdditionalFields cleans a configured list of extra field API names
// for tableName: it drops blanks, anything that isn't a bare field API name,
// duplicates, and names already selected by TableNamesToFieldsMapping. Each
// rejection is logged rather than returned as an error — a single typo should
// not stop a connector from starting.
func NormalizeAdditionalFields(ctx context.Context, tableName string, configured []string) []string {
	logger := ctxzap.Extract(ctx)

	seen := make(map[string]struct{}, len(configured))
	seen[strings.ToLower(SalesforcePK)] = struct{}{}
	for _, existing := range TableNamesToFieldsMapping[tableName] {
		seen[strings.ToLower(existing)] = struct{}{}
	}

	normalized := make([]string, 0, len(configured))
	for _, raw := range configured {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if !additionalFieldNamePattern.MatchString(name) {
			logger.Warn(
				"salesforce-client: ignoring invalid additional field name",
				zap.String("table", tableName),
				zap.String("field", name),
			)
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			logger.Debug(
				"salesforce-client: ignoring duplicate additional field name",
				zap.String("table", tableName),
				zap.String("field", name),
			)
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, name)

		if len(normalized) == maxAdditionalFields {
			logger.Warn(
				"salesforce-client: too many additional fields configured, ignoring the rest",
				zap.String("table", tableName),
				zap.Int("max", maxAdditionalFields),
			)
			break
		}
	}

	return normalized
}

// SetAdditionalUserFields records the extra User field API names to select
// alongside the standard ones. The list is normalized here; it is checked
// against the object's describe lazily, on first use.
func (c *SalesforceClient) SetAdditionalUserFields(ctx context.Context, fields []string) {
	normalized := NormalizeAdditionalFields(ctx, TableNameUsers, fields)

	c.additionalUserFieldsMutex.Lock()
	defer c.additionalUserFieldsMutex.Unlock()

	c.configuredUserFields = normalized
	c.clearAdditionalUserFieldResolutionLocked()
}

// clearAdditionalUserFieldResolutionLocked drops everything derived from the
// configured list, so the next call re-runs the describe. It does NOT touch
// configuredUserFields — that is the operator's config and only
// SetAdditionalUserFields replaces it.
func (c *SalesforceClient) clearAdditionalUserFieldResolutionLocked() {
	c.additionalUserFields = nil
	c.additionalUserFieldsResolved = false
	c.describeAttempts = 0
}

// ResetAdditionalUserFieldsForSync re-opens field resolution at the start of a
// full user sync.
//
// Without it, resolution is decided once and never revisited: a describe
// outage, or one INVALID_FIELD that trips the query-time fallback, would keep
// the extra fields off for the rest of the process. A hosted connector runs for
// days, so a customer who corrects a misspelled field name — or an admin who
// grants the integration user field-level access to one that was invisible —
// would see nothing change until someone restarted it. Re-resolving once per
// sync bounds the cost at one describe per sync while letting the feature
// recover on its own.
func (c *SalesforceClient) ResetAdditionalUserFieldsForSync() {
	c.additionalUserFieldsMutex.Lock()
	defer c.additionalUserFieldsMutex.Unlock()

	if len(c.configuredUserFields) == 0 {
		return
	}
	c.clearAdditionalUserFieldResolutionLocked()
}

// describeFieldNames returns the field API names of an SObject, keyed by their
// lowercased form so callers can match config case-insensitively (SOQL is).
// Only fields visible to the authenticated user are returned, which is exactly
// the set that can be selected.
func (c *SalesforceClient) describeFieldNames(ctx context.Context, tableName string) (map[string]string, error) {
	err := c.Initialize(ctx)
	if err != nil {
		return nil, err
	}

	body, err := c.client.ApexREST(
		ctx,
		http.MethodGet,
		fmt.Sprintf(DescribePathFmt, tableName),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("baton-salesforce: failed to describe %s: %w", tableName, err)
	}

	var described struct {
		Fields []struct {
			Name string `json:"name"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(body, &described); err != nil {
		return nil, fmt.Errorf("baton-salesforce: failed to parse %s describe response: %w", tableName, err)
	}

	names := make(map[string]string, len(described.Fields))
	for _, field := range described.Fields {
		if field.Name == "" {
			continue
		}
		names[strings.ToLower(field.Name)] = field.Name
	}
	return names, nil
}

// additionalUserFieldNames returns the configured extra User fields that are
// safe to select, resolving them against the User describe once.
//
// A configured field that doesn't exist on the object (a typo, or a field the
// integration user can't see) would make the whole user query fail with
// INVALID_FIELD, so unknown names are dropped with a warning instead. If the
// describe call itself fails we keep the configured list — losing the feature
// because of one flaky request would be worse, and GetUsers still falls back to
// the standard fields if Salesforce rejects the query.
//
// Resolution is only marked final once it has actually succeeded (or there is
// nothing to resolve), so a transient describe failure is retried on the next
// call rather than disabling canonical-casing resolution for the client's life.
// After maxDescribeAttempts failures we stop asking and settle for the
// configured names; additionalFieldValues matches record keys
// case-insensitively, so a mis-cased name still yields its value.
func (c *SalesforceClient) additionalUserFieldNames(ctx context.Context) []string {
	configured, done := c.additionalUserFieldsSnapshot()
	if done {
		return configured
	}

	// The describe runs WITHOUT the mutex. Holding it across a network
	// round-trip would park every concurrent caller — a parallel syncer, or a
	// GetUserByEmail on the provisioning path — behind one HTTP request, and a
	// describe that hangs would hold them until the client's timeout fires.
	// Racing callers may each issue a describe; one duplicate GET is a far
	// better trade than serializing them. Initialize, which describeFieldNames
	// calls, does not touch this mutex (salesforce.go:83).
	logger := ctxzap.Extract(ctx)
	available, err := c.describeFieldNames(ctx, TableNameUsers)

	return c.storeAdditionalUserFields(logger, available, err)
}

// additionalUserFieldsSnapshot reports the fields to use and whether resolution
// is already settled. When it returns false the caller must run a describe and
// hand the outcome to storeAdditionalUserFields.
func (c *SalesforceClient) additionalUserFieldsSnapshot() ([]string, bool) {
	c.additionalUserFieldsMutex.Lock()
	defer c.additionalUserFieldsMutex.Unlock()

	if c.additionalUserFieldsResolved {
		return c.additionalUserFields, true
	}
	if len(c.configuredUserFields) == 0 {
		c.additionalUserFieldsResolved = true
		return nil, true
	}
	return nil, false
}

// storeAdditionalUserFields folds a describe result into the client's state and
// returns the fields to select.
func (c *SalesforceClient) storeAdditionalUserFields(
	logger *zap.Logger,
	available map[string]string,
	describeErr error,
) []string {
	c.additionalUserFieldsMutex.Lock()
	defer c.additionalUserFieldsMutex.Unlock()

	// While we were out of the lock another goroutine may have resolved the
	// list, or the query-time fallback may have switched the fields off. Either
	// way that decision wins over this describe.
	if c.additionalUserFieldsResolved {
		return c.additionalUserFields
	}
	if len(c.configuredUserFields) == 0 {
		c.additionalUserFieldsResolved = true
		return nil
	}

	if describeErr != nil {
		c.describeAttempts++
		exhausted := c.describeAttempts >= maxDescribeAttempts
		logger.Warn(
			"salesforce-client: could not describe the User object, using additional fields as configured",
			zap.Strings("additional_fields", c.configuredUserFields),
			zap.Int("attempt", c.describeAttempts),
			zap.Bool("will_retry", !exhausted),
			zap.Error(describeErr),
		)
		if exhausted {
			c.additionalUserFields = c.configuredUserFields
			c.additionalUserFieldsResolved = true
		}
		return c.configuredUserFields
	}

	resolved := make([]string, 0, len(c.configuredUserFields))
	for _, name := range c.configuredUserFields {
		canonical, ok := available[strings.ToLower(name)]
		if !ok {
			logger.Warn(
				"salesforce-client: additional field does not exist on the User object or is not visible to this user, skipping it",
				zap.String("field", name),
			)
			continue
		}
		resolved = append(resolved, canonical)
	}

	logger.Info(
		"salesforce-client: syncing additional User fields",
		zap.Strings("additional_fields", resolved),
	)
	c.additionalUserFields = resolved
	c.additionalUserFieldsResolved = true
	return c.additionalUserFields
}

// disableAdditionalUserFields drops the extra fields for the rest of the
// current sync. Called when Salesforce rejects them at query time, so that
// every later page and every later query uses the standard field set.
//
// configuredUserFields is deliberately left alone: the next
// ResetAdditionalUserFieldsForSync re-resolves from it, so a corrected field
// name or a newly granted permission takes effect on the next sync instead of
// needing a process restart.
func (c *SalesforceClient) disableAdditionalUserFields() {
	c.additionalUserFieldsMutex.Lock()
	defer c.additionalUserFieldsMutex.Unlock()

	c.additionalUserFields = nil
	c.additionalUserFieldsResolved = true
	c.describeAttempts = maxDescribeAttempts
}

// userSelectFields is the SELECT list for a User query: the standard fields
// plus the given additional ones.
func userSelectFields(additional []string) []string {
	standard := TableNamesToFieldsMapping[TableNameUsers]

	fields := make([]string, 0, len(standard)+len(additional))
	fields = append(fields, standard...)
	fields = append(fields, additional...)
	return fields
}

// isInvalidFieldError reports whether Salesforce rejected a query because one of
// the selected fields doesn't exist on the object (or isn't visible to the
// authenticated user).
func isInvalidFieldError(err error) bool {
	var sfErr simpleforce.SalesforceError
	return errors.As(err, &sfErr) && sfErr.ErrorCode == "INVALID_FIELD"
}

// additionalFieldValues reads the configured extra fields off a User record.
// Values are passed through with their JSON types so a checkbox stays a
// boolean and a number stays a number. Empty and null values are omitted — an
// unset picklist carries no information for a reviewer — and structured values
// (a nested relationship object) are skipped because the user profile is flat.
//
// Values are keyed by the record's own field name, which Salesforce always
// reports as the canonical field API name whatever casing the SELECT used. So
// the profile key is the canonical name whether or not the describe check ran.
func additionalFieldValues(
	ctx context.Context,
	record simpleforce.SObject,
	fields []string,
) map[string]any {
	if len(fields) == 0 {
		return nil
	}

	logger := ctxzap.Extract(ctx)
	// Built lazily, and only if some field misses an exact match: lowercased
	// record key -> the key as Salesforce spelled it.
	var recordKeys map[string]string

	values := make(map[string]any, len(fields))
	for _, name := range fields {
		key := name
		if _, present := record[key]; !present {
			// A name that never went through the describe check keeps whatever
			// casing config used, so match the record's keys case-insensitively
			// (SOQL is case-insensitive, the JSON response is not).
			if recordKeys == nil {
				recordKeys = make(map[string]string, len(record))
				for recordKey := range record {
					recordKeys[strings.ToLower(recordKey)] = recordKey
				}
			}
			canonical, ok := recordKeys[strings.ToLower(name)]
			if !ok {
				logger.Debug(
					"salesforce-client: additional field is absent from the User record",
					zap.String("field", name),
					zap.String("user_id", record.ID()),
				)
				continue
			}
			key = canonical
		}

		switch value := record.InterfaceField(key).(type) {
		case nil:
			// Field is unset for this user.
		case string:
			if value == "" {
				continue
			}
			values[key] = value
		case bool:
			values[key] = value
		case float64:
			// Every numeric Salesforce value arrives here: records are decoded
			// by encoding/json into map[string]any, which represents all JSON
			// numbers as float64.
			values[key] = value
		default:
			logger.Debug(
				"salesforce-client: skipping additional field with unsupported value type",
				zap.String("field", key),
				zap.String("user_id", record.ID()),
			)
		}
	}

	if len(values) == 0 {
		return nil
	}
	return values
}
