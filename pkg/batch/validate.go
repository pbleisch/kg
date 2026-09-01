package batch

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/pbleisch/kg/pkg/schema"
	"github.com/pbleisch/kg/pkg/store"
)

// A RecordError is one structural refusal, tied to the record that caused it.
type RecordError struct {
	Record  string `json:"record"`
	Index   int    `json:"index"`
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

func (e RecordError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Record, e.Message, e.Field)
	}
	return fmt.Sprintf("%s: %s", e.Record, e.Message)
}

// A Warning is advisory: reported, never blocking.  Content is advised (P3).
type Warning struct {
	Record  string `json:"record"`
	Index   int    `json:"index"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// A RefusedError reports a batch that failed structural validation.  Nothing
// was written.
type RefusedError struct {
	Errors []RecordError
}

func (e *RefusedError) Error() string {
	msgs := make([]string, 0, len(e.Errors))
	for _, re := range e.Errors {
		msgs = append(msgs, re.Error())
	}
	return fmt.Sprintf("batch refused: %d structural error(s): %s",
		len(e.Errors), strings.Join(msgs, "; "))
}

// Refused reports whether err is a structural refusal, and returns its errors.
func Refused(err error) ([]RecordError, bool) {
	var re *RefusedError
	if errors.As(err, &re) {
		return re.Errors, true
	}
	return nil, false
}

// validate runs every structural rule over the whole batch before the first
// write.  It returns hard errors and advisory warnings separately, because
// they are not the same rule and must not share a setting.
func validate(kgDir string, records []Record) ([]RecordError, []Warning) {
	var errs []RecordError
	var warns []Warning

	fail := func(i int, r Record, code, field, msg string) {
		errs = append(errs, RecordError{
			Record: r.label(), Index: i, Code: code, Field: field, Message: msg,
		})
	}

	// Identities this batch declares, and the record that first declared each.
	declared := make(map[string]int, len(records))

	for i, r := range records {
		if id := r.entityID(); id != "" {
			switch r.Op {
			case OpCreate, OpUpsert:
				if _, dup := declared[id]; !dup {
					declared[id] = i
				}
			}
		}
	}

	for i, r := range records {
		switch r.Op {
		case OpCreate, OpUpsert:
			e := r.Entity
			if e == nil || e.ID == "" {
				fail(i, r, "missing_entity", "entity", "record declares no entity id")
				continue
			}
			typeName, localID, err := store.ParseEntityID(e.ID)
			if err != nil {
				fail(i, r, "bad_id", "entity.id", err.Error())
				continue
			}
			if e.Type == "" {
				fail(i, r, "missing_type", "entity.type", "record declares no type")
			} else if e.Type != typeName {
				fail(i, r, "type_disagreement", "entity.type", fmt.Sprintf(
					"declared type %q disagrees with id %q, which stores under %q",
					e.Type, e.ID, typeName))
			}
			if localID == "" {
				fail(i, r, "bad_id", "entity.id", "empty local id")
			}
			// Identity uniqueness, within the batch — for the records the
			// fold does not take.  Two upserts of one id are folded into one
			// record before this runs (see fold), so what is left here is a
			// `create` under on-conflict=fail, which asserts novelty and must
			// keep that assertion.  The other half of P3 — two *files*
			// claiming one identity — is the load-time check.
			if first := declared[e.ID]; first != i {
				fail(i, r, "identity_collision", "entity.id", fmt.Sprintf(
					"identity %q is declared by record %d (%s) and record %d (%s)",
					e.ID, first, records[first].label(), i, r.label()))
			}
			if err := store.ValidateAttributeNames(e.Attributes); err != nil {
				fail(i, r, "illegal_attribute", "entity.attributes", err.Error())
			}
			for rel, targets := range e.Relations {
				if rel == "" {
					fail(i, r, "bad_relation", "entity.relations", "empty relation name")
				}
				for _, t := range targets {
					if !resolves(kgDir, t, declared) {
						fail(i, r, "dangling_relation", "entity.relations."+rel, fmt.Sprintf(
							"relation %q points at %q, which no record declares and the store does not hold",
							rel, t))
					}
				}
			}
			warns = append(warns, contentWarnings(kgDir, i, r, e)...)

		case OpSet:
			if r.Target == "" {
				fail(i, r, "missing_target", "target", "set names no target")
				continue
			}
			if len(r.Attributes) == 0 {
				fail(i, r, "empty_set", "attributes", "set carries no attributes")
			}
			if err := store.ValidateAttributeNames(r.Attributes); err != nil {
				fail(i, r, "illegal_attribute", "attributes", err.Error())
			}
			if !resolves(kgDir, r.Target, declared) {
				fail(i, r, "unresolved_target", "target", fmt.Sprintf(
					"target %q does not exist and no record declares it", r.Target))
			}

		case OpUnset:
			if r.Target == "" {
				fail(i, r, "missing_target", "target", "unset names no target")
				continue
			}
			if len(r.Unset) == 0 {
				fail(i, r, "empty_unset", "unset", "unset names no attributes")
			}
			for _, name := range r.Unset {
				if !store.ValidAttributeName(name) {
					fail(i, r, "illegal_attribute", "unset", fmt.Sprintf(
						"%q is not a legal attribute name (must match [a-z][a-z0-9_]*)", name))
				}
			}
			if !resolves(kgDir, r.Target, declared) {
				fail(i, r, "unresolved_target", "target", fmt.Sprintf(
					"target %q does not exist and no record declares it", r.Target))
			}

		case OpLink, OpUnlink:
			if r.From == "" || r.Rel == "" || r.To == "" {
				fail(i, r, "incomplete_link", "", "link needs from, rel and to")
				continue
			}
			// Both ends must resolve.  Minting a stub for a missing end would
			// make the boundary itself a producer of entities nobody submitted.
			if !resolves(kgDir, r.From, declared) {
				fail(i, r, "dangling_relation", "from", fmt.Sprintf(
					"source %q does not exist and no record declares it", r.From))
			}
			if !resolves(kgDir, r.To, declared) {
				fail(i, r, "dangling_relation", "to", fmt.Sprintf(
					"target %q does not exist and no record declares it", r.To))
			}

		case OpDelete:
			if r.Target == "" {
				fail(i, r, "missing_target", "target", "delete names no target")
			}

		case OpMerge:
			if r.From == "" || r.Target == "" {
				fail(i, r, "incomplete_merge", "", "merge needs from and target")
				continue
			}
			if r.Prefer != "" && r.Prefer != "source" && r.Prefer != "target" {
				fail(i, r, "bad_prefer", "prefer", fmt.Sprintf(
					"prefer %q is neither \"source\" nor \"target\"", r.Prefer))
			}

		case "":
			fail(i, r, "missing_op", "action", "record names no action")

		default:
			fail(i, r, "unknown_op", "action", fmt.Sprintf("unknown action %q", r.Op))
		}
	}

	sort.SliceStable(errs, func(a, b int) bool { return errs[a].Index < errs[b].Index })
	return errs, warns
}

// resolves reports whether ref names something this batch declares or the store
// already holds.
func resolves(kgDir, ref string, declared map[string]int) bool {
	if ref == "" {
		return false
	}
	if _, ok := declared[ref]; ok {
		return true
	}
	if _, _, err := store.ParseEntityID(ref); err != nil {
		return false
	}
	path := store.EntityPath(kgDir, ref)
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// contentWarnings runs the schema's content rules, which never block.
func contentWarnings(kgDir string, i int, r Record, e *store.Entity) []Warning {
	s, err := schema.LoadSchema(kgDir, e.Type)
	if err != nil || s == nil {
		return nil
	}
	verrs := schema.ValidateEntity(s, e)
	if len(verrs) == 0 {
		return nil
	}
	out := make([]Warning, 0, len(verrs))
	for _, ve := range verrs {
		out = append(out, Warning{
			Record: r.label(), Index: i, Code: "content", Message: ve.Error(),
		})
	}
	return out
}
