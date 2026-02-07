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
