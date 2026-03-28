package output

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/pbleisch/kg/pkg/store"
)

type humanFormatter struct{}

func (f *humanFormatter) FormatEntity(w io.Writer, e *store.Entity) error {
	// Header line: ═══ Type: id ═══
	label := fmt.Sprintf(" %s: %s ", e.Type, e.ID)
	pad := strings.Repeat("═", 3)
	fmt.Fprintf(w, "%s%s%s\n", pad, label, pad)

	// Sorted attributes with aligned values.
	attrKeys := make([]string, 0, len(e.Attributes))
	for k := range e.Attributes {
		attrKeys = append(attrKeys, k)
	}
	sort.Strings(attrKeys)

	maxKeyLen := 0
	for _, k := range attrKeys {
		if len(k) > maxKeyLen {
			maxKeyLen = len(k)
		}
	}

	for _, k := range attrKeys {
		label := k + ":"
		fmt.Fprintf(w, "  %-*s %v\n", maxKeyLen+1, label, e.Attributes[k])
	}

	// Relations.
	if len(e.Relations) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "  Relations:")

		relKeys := make([]string, 0, len(e.Relations))
		for k := range e.Relations {
			relKeys = append(relKeys, k)
		}
		sort.Strings(relKeys)

		for _, k := range relKeys {
			for _, target := range e.Relations[k] {
				fmt.Fprintf(w, "    %s → %s\n", k, target)
			}
		}
	}

	return nil
}

func (f *humanFormatter) FormatEntities(w io.Writer, entities []*store.Entity) error {
	if len(entities) == 0 {
		fmt.Fprintln(w, "No results.")
		return nil
	}

	for i, e := range entities {
		if i > 0 {
			fmt.Fprintln(w)
		}
		if err := f.FormatEntity(w, e); err != nil {
			return err
		}
	}

	fmt.Fprintf(w, "\n(%d entities)\n", len(entities))
	return nil
}

func (f *humanFormatter) FormatError(w io.Writer, code string, message string, suggestions []string) error {
	fmt.Fprintf(w, "Error [%s]: %s\n", code, message)
	if len(suggestions) > 0 {
		fmt.Fprintln(w, "  Did you mean:")
		for _, s := range suggestions {
			fmt.Fprintf(w, "    • %s\n", s)
		}
	}
	return nil
}

func (f *humanFormatter) FormatSchema(w io.Writer, s SchemaInfo) error {
	label := fmt.Sprintf(" %s ", s.Type)
	pad := strings.Repeat("═", 3)
	fmt.Fprintf(w, "%s%s%s\n", pad, label, pad)

	if s.Description != "" {
		fmt.Fprintf(w, "  %s\n", s.Description)
		fmt.Fprintln(w)
	}

	if len(s.Attributes) > 0 {
		fmt.Fprintln(w, "  Attributes:")
		maxLen := 0
		for _, a := range s.Attributes {
			if len(a.Name) > maxLen {
				maxLen = len(a.Name)
			}
		}
		for _, a := range s.Attributes {
			req := ""
			if a.Required {
				req = " (required)"
			}
			fmt.Fprintf(w, "    %-*s  %s%s\n", maxLen, a.Name, a.Type, req)
		}
	}

	if len(s.Relations) > 0 {
		if len(s.Attributes) > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, "  Relations:")
		for _, r := range s.Relations {
			fmt.Fprintf(w, "    %s → %s [%s]\n", r.Name, r.Target, r.Cardinality)
		}
	}

	return nil
}

func (f *humanFormatter) FormatTypeList(w io.Writer, names []string) error {
	fmt.Fprintf(w, "Schema Types (%d):\n", len(names))
	for _, name := range names {
		fmt.Fprintf(w, "  • %s\n", name)
	}
	return nil
}

func (f *humanFormatter) FormatPathIDs(w io.Writer, ids []string) error {
	for _, id := range ids {
		fmt.Fprintln(w, id)
	}
	if len(ids) > 1 {
		fmt.Fprintf(w, "\n(%d results)\n", len(ids))
	}
	return nil
}

func (f *humanFormatter) FormatPathVerbose(w io.Writer, results []PathResult) error {
	for _, r := range results {
		fmt.Fprintf(w, "%s%s (via %s)\n",
			strings.Repeat("  ", r.Depth-1), r.ID, r.Via)
	}
	if len(results) > 1 {
		fmt.Fprintf(w, "\n(%d results)\n", len(results))
	}
	return nil
}
