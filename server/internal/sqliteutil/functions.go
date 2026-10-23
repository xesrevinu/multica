package sqliteutil

import (
	"database/sql/driver"
	"encoding/json"
	"hash/fnv"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"modernc.org/sqlite"
)

const sqliteTimeFormat = "2006-01-02T15:04:05.000000000Z"

var (
	configMu      sync.RWMutex
	sessionConfig = map[string]string{}
	localConfig   = map[string]string{}
)

// ClearLocalConfig clears transaction-local settings (set_config(..., is_local=true)).
func ClearLocalConfig() {
	configMu.Lock()
	clear(localConfig)
	configMu.Unlock()
}

func parseSQLiteTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if strings.EqualFold(s, "now") {
		return time.Now().UTC(), true
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999Z",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func applyModifier(t time.Time, mod string) (time.Time, bool) {
	mod = strings.TrimSpace(mod)
	if mod == "" || strings.EqualFold(mod, "utc") {
		return t, true
	}
	fields := strings.Fields(mod)
	if len(fields) != 2 {
		return t, false
	}
	val, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return t, false
	}
	unit := strings.ToLower(strings.TrimSuffix(fields[1], "s"))
	var dur time.Duration
	switch unit {
	case "second":
		dur = time.Duration(val * float64(time.Second))
	case "minute":
		dur = time.Duration(val * float64(time.Minute))
	case "hour":
		dur = time.Duration(val * float64(time.Hour))
	case "day":
		dur = time.Duration(val * 24 * float64(time.Hour))
	default:
		return t, false
	}
	return t.Add(dur), true
}

func toFloat(v driver.Value) (float64, bool) {
	switch x := v.(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func compareValues(a, b driver.Value) int {
	if sa, oka := a.(string); oka {
		if sb, okb := b.(string); okb {
			if ta, okta := parseSQLiteTime(sa); okta {
				if tb, oktb := parseSQLiteTime(sb); oktb {
					if ta.Before(tb) {
						return -1
					}
					if ta.After(tb) {
						return 1
					}
					return 0
				}
			}
			return strings.Compare(sa, sb)
		}
	}
	if fa, oka := toFloat(a); oka {
		if fb, okb := toFloat(b); okb {
			if fa < fb {
				return -1
			}
			if fa > fb {
				return 1
			}
			return 0
		}
	}
	return strings.Compare(fmt.Sprint(a), fmt.Sprint(b))
}

// init registers SQLite stand-ins for the Postgres functions the schema and
// queries call. modernc.org/sqlite applies driver-level scalar functions to
// every connection opened afterwards (sqlite.MustRegisterScalarFunction).
func init() {
	sqlite.MustRegisterScalarFunction(
		"gen_random_uuid",
		0,
		func(_ *sqlite.FunctionContext, _ []driver.Value) (driver.Value, error) {
			return uuid.NewString(), nil
		},
	)
	sqlite.MustRegisterScalarFunction(
		"now",
		0,
		func(_ *sqlite.FunctionContext, _ []driver.Value) (driver.Value, error) {
			return time.Now().UTC().Format(sqliteTimeFormat), nil
		},
	)
	sqlite.MustRegisterScalarFunction(
		"datetime",
		-1,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			if len(args) == 0 || args[0] == nil {
				return nil, nil
			}
			var base time.Time
			modStart := 1
			if len(args) >= 2 && fmt.Sprint(args[1]) == "unixepoch" {
				sec, ok := toFloat(args[0])
				if !ok {
					return nil, nil
				}
				nsec := int64((sec - math.Floor(sec)) * 1e9)
				base = time.Unix(int64(math.Floor(sec)), nsec).UTC()
				modStart = 2
			} else {
				var ok bool
				base, ok = parseSQLiteTime(fmt.Sprint(args[0]))
				if !ok {
					return nil, nil
				}
			}
			for i := modStart; i < len(args); i++ {
				if args[i] == nil {
					return nil, nil
				}
				var ok bool
				base, ok = applyModifier(base, fmt.Sprint(args[i]))
				if !ok {
					return nil, nil
				}
			}
			return base.UTC().Format(sqliteTimeFormat), nil
		},
	)
	sqlite.MustRegisterScalarFunction(
		"greatest",
		-1,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			var best driver.Value
			for _, a := range args {
				if a == nil {
					continue
				}
				if best == nil || compareValues(a, best) > 0 {
					best = a
				}
			}
			return best, nil
		},
	)
	sqlite.MustRegisterScalarFunction(
		"least",
		-1,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			var best driver.Value
			for _, a := range args {
				if a == nil {
					continue
				}
				if best == nil || compareValues(a, best) < 0 {
					best = a
				}
			}
			return best, nil
		},
	)
	sqlite.MustRegisterScalarFunction(
		"power",
		2,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			if len(args) < 2 || args[0] == nil || args[1] == nil {
				return nil, nil
			}
			a, oka := toFloat(args[0])
			b, okb := toFloat(args[1])
			if !oka || !okb {
				return nil, nil
			}
			return math.Pow(a, b), nil
		},
	)
	sqlite.MustRegisterScalarFunction(
		"set_config",
		3,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			if len(args) < 2 || args[0] == nil {
				return "", nil
			}
			key := fmt.Sprint(args[0])
			val := ""
			if args[1] != nil {
				val = fmt.Sprint(args[1])
			}
			isLocal := true
			if len(args) >= 3 && args[2] != nil {
				switch v := args[2].(type) {
				case bool:
					isLocal = v
				case int64:
					isLocal = v != 0
				case string:
					isLocal = v != "false" && v != "0" && v != "f"
				}
			}
			configMu.Lock()
			if isLocal {
				localConfig[key] = val
			} else {
				sessionConfig[key] = val
			}
			configMu.Unlock()
			return val, nil
		},
	)
	sqlite.MustRegisterScalarFunction(
		"current_setting",
		-1,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			if len(args) == 0 || args[0] == nil {
				return "", nil
			}
			key := fmt.Sprint(args[0])
			configMu.RLock()
			val, ok := localConfig[key]
			if !ok {
				val = sessionConfig[key]
			}
			configMu.RUnlock()
			return val, nil
		},
	)
	sqlite.MustRegisterScalarFunction(
		"issue_status_category",
		1,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			if len(args) == 0 || args[0] == nil {
				return nil, nil
			}
			s, ok := args[0].(string)
			if !ok {
				return args[0], nil
			}
			switch s {
			case "backlog", "todo", "unstarted":
				return "unstarted", nil
			case "in_progress", "in_review", "blocked", "started":
				return "started", nil
			case "done":
				return "done", nil
			case "cancelled", "closed":
				return "closed", nil
			default:
				return s, nil
			}
		},
	)
	sqlite.MustRegisterScalarFunction(
		"cardinality",
		1,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			if len(args) == 0 || args[0] == nil {
				return nil, nil
			}
			raw := strings.TrimSpace(fmt.Sprint(args[0]))
			if raw == "" || raw == "{}" || raw == "[]" || raw == "null" {
				return int64(0), nil
			}
			var arr []any
			if err := json.Unmarshal([]byte(raw), &arr); err == nil {
				return int64(len(arr)), nil
			}
			if strings.HasPrefix(raw, "{") && strings.HasSuffix(raw, "}") {
				inner := raw[1 : len(raw)-1]
				if inner == "" {
					return int64(0), nil
				}
				return int64(len(strings.Split(inner, ","))), nil
			}
			return int64(0), nil
		},
	)
	sqlite.MustRegisterScalarFunction(
		"jsonb_typeof",
		1,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			if len(args) == 0 || args[0] == nil {
				return nil, nil
			}
			switch v := args[0].(type) {
			case int64, float64:
				return "number", nil
			case bool:
				return "boolean", nil
			case string:
				raw := strings.TrimSpace(v)
				if raw == "" {
					return nil, nil
				}
				switch {
				case raw == "null":
					return "null", nil
				case raw == "true" || raw == "false":
					return "boolean", nil
				case strings.HasPrefix(raw, `"`):
					return "string", nil
				case strings.HasPrefix(raw, "["):
					return "array", nil
				case strings.HasPrefix(raw, "{"):
					return "object", nil
				default:
					if _, err := strconv.ParseFloat(raw, 64); err == nil {
						return "number", nil
					}
					return "string", nil
				}
			default:
				return nil, nil
			}
		},
	)
	sqlite.MustRegisterScalarFunction(
		"hashtext",
		1,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			if len(args) == 0 || args[0] == nil {
				return int64(0), nil
			}
			h := fnv.New32a()
			_, _ = h.Write([]byte(fmt.Sprint(args[0])))
			return int64(int32(h.Sum32())), nil
		},
	)
	sqlite.MustRegisterScalarFunction(
		"hashtextextended",
		2,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			if len(args) == 0 || args[0] == nil {
				return int64(0), nil
			}
			h := fnv.New64a()
			_, _ = h.Write([]byte(fmt.Sprint(args[0])))
			return int64(h.Sum64()), nil
		},
	)
	for _, jbName := range []string{"jsonb_build_object", "json_build_object"} {
		name := jbName
		sqlite.MustRegisterScalarFunction(
			name,
			-1,
			func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
				m := make(map[string]any, len(args)/2)
				for i := 0; i+1 < len(args); i += 2 {
					k := fmt.Sprint(args[i])
					m[k] = args[i+1]
				}
				b, err := json.Marshal(m)
				if err != nil {
					return "{}", nil
				}
				return string(b), nil
			},
		)
	}
	for _, lockFn := range []string{"pg_advisory_xact_lock", "pg_advisory_lock", "pg_advisory_unlock", "pg_try_advisory_xact_lock", "pg_try_advisory_lock"} {
		fnName := lockFn
		sqlite.MustRegisterScalarFunction(
			fnName,
			-1,
			func(_ *sqlite.FunctionContext, _ []driver.Value) (driver.Value, error) {
				return int64(1), nil
			},
		)
	}
}
