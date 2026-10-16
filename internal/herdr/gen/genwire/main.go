// Command genwire generates herddash's herdr wire types from the API schema that
// `herdr api schema --json` prints. It is run by `go generate ./internal/herdr/gen`
// and imports only the standard library.
//
// It generates only the definitions herddash needs, named in roots below, plus
// everything they reference. The mapping is:
//
//   - an object becomes a struct; a required, non-nullable property is a value,
//     and an optional or nullable one is a pointer tagged omitempty (slices, maps
//     and json.RawMessage are already nilable and are never pointers);
//   - a string enum becomes a named string type plus one constant per value;
//   - a $ref is resolved across the schema's sections, and a definition that
//     appears in several sections is generated once, after checking that every
//     copy is identical;
//   - a discriminated union (a oneOf whose variants each fix one property with
//     const) becomes one struct per selected variant, with the parent's own
//     properties merged in, plus a named type and constants for the
//     discriminator;
//   - a property whose schema is any other union, or a union decoded later from
//     its discriminator, is a json.RawMessage.
//
// Output is deterministic: types are sorted by name and fields by JSON name.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// root names one definition to generate. An empty def means the section's
// top-level schema, named by its title. For a discriminated union, variants
// selects which discriminator values are generated; every one must exist.
type root struct {
	section  string
	def      string
	variants []string
}

// roots is everything herddash needs from herdr. It deliberately has no focus
// method and no pane-output method or subscription (AD-5); forbidden enforces it.
var roots = []root{
	{section: "request", variants: []string{"ping", "session.snapshot", "events.subscribe", "pane.process_info"}},
	{section: "request", def: "Subscription", variants: []string{"pane.created", "pane.closed", "pane.moved", "pane.exited", "pane.agent_detected", "pane.agent_status_changed",
		"workspace.renamed", "workspace.moved", "workspace.reordered", "tab.renamed", "tab.moved", "pane.updated"}},
	{section: "success_response"},
	{section: "success_response", def: "ResponseResult", variants: []string{"pong", "session_snapshot", "subscription_started", "pane_process_info"}},
	{section: "error_response"},
	{section: "event"},
	{section: "event", def: "EventData", variants: []string{"pane_created", "pane_closed", "pane_moved", "pane_exited", "pane_agent_detected"}},
	{section: "subscription_event"},
	{section: "subscription_event", def: "PaneAgentStatusChangedEvent"},
	{section: "event", def: "AgentStatus"},
}

// forbidden are discriminator values genwire refuses to generate, whatever roots
// says: focus mutates herdr's seen state, and herddash never reads pane output.
var forbidden = map[string]bool{
	"pane.focus": true, "agent.focus": true, "pane.read": true, "agent.read": true,
	"pane.output_matched": true, "pane.wait_for_output": true,
}

// schema is the subset of JSON Schema 2020-12 that herdr's dump uses.
type schema struct {
	Ref                  string             `json:"$ref"`
	Type                 json.RawMessage    `json:"type"`
	Const                json.RawMessage    `json:"const"`
	Enum                 []json.RawMessage  `json:"enum"`
	Format               string             `json:"format"`
	Description          string             `json:"description"`
	Title                string             `json:"title"`
	Properties           map[string]*schema `json:"properties"`
	Required             []string           `json:"required"`
	Items                *schema            `json:"items"`
	AdditionalProperties json.RawMessage    `json:"additionalProperties"`
	OneOf                []*schema          `json:"oneOf"`
	AnyOf                []*schema          `json:"anyOf"`
	Defs                 map[string]*schema `json:"$defs"`

	// boolean is set for the boolean schemas true and false, which herdr uses
	// for a property of any value.
	boolean bool
}

func (s *schema) UnmarshalJSON(b []byte) error {
	if t := string(bytes.TrimSpace(b)); t == "true" || t == "false" {
		*s = schema{boolean: true}
		return nil
	}
	type plain schema
	return json.Unmarshal(b, (*plain)(s))
}

type document struct {
	Protocol      int                `json:"protocol"`
	SchemaVersion int                `json:"schema_version"`
	Schemas       map[string]*schema `json:"schemas"`
}

func main() {
	schemaPath := flag.String("schema", "herdr-api-0.9.5.schema.json", "herdr API schema dump")
	out := flag.String("out", "wire_gen.go", "output file")
	pkg := flag.String("package", "gen", "package name of the output")
	flag.Parse()

	in, err := os.ReadFile(*schemaPath)
	if err != nil {
		fail(err)
	}
	src, err := generate(in, filepath.Base(*schemaPath), *pkg)
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(*out, src, 0o644); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "genwire:", err)
	os.Exit(1)
}

// generator holds the rendered types, keyed by Go name.
type generator struct {
	doc     document
	types   map[string]string // Go name -> rendered declaration
	pending []defRef
	done    map[defRef]bool
}

type defRef struct{ section, name string }

func generate(in []byte, source, pkg string) ([]byte, error) {
	g := &generator{types: map[string]string{}, done: map[defRef]bool{}}
	if err := json.Unmarshal(in, &g.doc); err != nil {
		return nil, fmt.Errorf("decode schema: %w", err)
	}
	for _, r := range roots {
		if err := g.root(r); err != nil {
			return nil, err
		}
	}
	for len(g.pending) > 0 {
		d := g.pending[0]
		g.pending = g.pending[1:]
		if err := g.def(d); err != nil {
			return nil, err
		}
	}

	names := make([]string, 0, len(g.types))
	for n := range g.types {
		names = append(names, n)
	}
	sort.Strings(names)

	var b bytes.Buffer
	fmt.Fprintf(&b, "// Code generated by genwire from %s (herdr API schema_version %d, protocol %d). DO NOT EDIT.\n\n",
		source, g.doc.SchemaVersion, g.doc.Protocol)
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	body := strings.Join(func() []string {
		out := make([]string, len(names))
		for i, n := range names {
			out[i] = g.types[n]
		}
		return out
	}(), "\n")
	if strings.Contains(body, "json.RawMessage") {
		b.WriteString("import \"encoding/json\"\n\n")
	}
	b.WriteString(body)
	src, err := format.Source(b.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format output: %w\n%s", err, b.Bytes())
	}
	return src, nil
}

func (g *generator) section(name string) (*schema, error) {
	s, ok := g.doc.Schemas[name]
	if !ok {
		return nil, fmt.Errorf("schema has no section %q", name)
	}
	return s, nil
}

func (g *generator) lookup(d defRef) (*schema, error) {
	sec, err := g.section(d.section)
	if err != nil {
		return nil, err
	}
	s, ok := sec.Defs[d.name]
	if !ok {
		return nil, fmt.Errorf("section %s has no definition %q", d.section, d.name)
	}
	return s, nil
}

// parseRef splits "#/schemas/<section>/$defs/<Name>".
func parseRef(ref string) (defRef, error) {
	parts := strings.Split(strings.TrimPrefix(ref, "#/"), "/")
	if len(parts) != 4 || parts[0] != "schemas" || parts[2] != "$defs" {
		return defRef{}, fmt.Errorf("unsupported $ref %q", ref)
	}
	return defRef{parts[1], parts[3]}, nil
}

func (g *generator) root(r root) error {
	if r.def == "" {
		s, err := g.section(r.section)
		if err != nil {
			return err
		}
		if s.Title == "" {
			return fmt.Errorf("section %s has no title to name it", r.section)
		}
		return g.emit(r.section, s.Title, s, r.variants)
	}
	d := defRef{r.section, r.def}
	s, err := g.lookup(d)
	if err != nil {
		return err
	}
	g.done[d] = true
	return g.emit(r.section, r.def, s, r.variants)
}

func (g *generator) def(d defRef) error {
	s, err := g.lookup(d)
	if err != nil {
		return err
	}
	return g.emit(d.section, d.name, s, nil)
}

func (g *generator) need(d defRef) {
	if !g.done[d] {
		g.done[d] = true
		g.pending = append(g.pending, d)
	}
}

// put records a rendered declaration, refusing two different ones under one name.
func (g *generator) put(name, decl string) error {
	if prev, ok := g.types[name]; ok && prev != decl {
		return fmt.Errorf("definition %s differs between schema sections", name)
	}
	g.types[name] = decl
	return nil
}

func (g *generator) emit(section, name string, s *schema, variants []string) error {
	switch {
	case len(s.Enum) > 0:
		return g.emitEnum(name, s)
	case len(s.OneOf) > 0:
		if variants == nil {
			return fmt.Errorf("%s is a union and is generated only as a root with chosen variants", name)
		}
		return g.emitUnion(section, name, s, variants)
	case len(s.AnyOf) > 0:
		return fmt.Errorf("%s is an undiscriminated union; generate its members as roots instead", name)
	}
	if variants != nil {
		return fmt.Errorf("%s is not a discriminated union, so it takes no variants", name)
	}
	types, _ := typeList(s.Type)
	if len(types) != 1 || types[0] != "object" {
		return fmt.Errorf("%s: unsupported top-level schema type %v", name, types)
	}
	decl, err := g.renderStruct(section, name, docLines(name, s.Description, generatedFrom), s.Properties, s.Required, "", "")
	if err != nil {
		return err
	}
	return g.put(name, decl)
}

func (g *generator) emitEnum(name string, s *schema) error {
	types, _ := typeList(s.Type)
	if len(types) != 1 || types[0] != "string" {
		return fmt.Errorf("%s: only string enums are supported", name)
	}
	var b strings.Builder
	for _, l := range docLines(name, s.Description, "is a herdr string enum.") {
		fmt.Fprintf(&b, "// %s\n", l)
	}
	fmt.Fprintf(&b, "type %s string\n\n", name)
	fmt.Fprintf(&b, "// %s values.\nconst (\n", name)
	for _, raw := range s.Enum {
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return fmt.Errorf("%s: non-string enum value %s", name, raw)
		}
		fmt.Fprintf(&b, "\t%s%s %s = %q\n", name, pascal(v), name, v)
	}
	b.WriteString(")\n")
	return g.put(name, b.String())
}

func (g *generator) emitUnion(section, name string, s *schema, variants []string) error {
	disc, err := discriminator(s.OneOf)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	byValue := map[string]*schema{}
	for _, v := range s.OneOf {
		var c string
		if err := json.Unmarshal(v.Properties[disc].Const, &c); err != nil {
			return fmt.Errorf("%s: non-string discriminator: %w", name, err)
		}
		byValue[c] = v
	}

	discType := name + pascal(disc)
	var consts strings.Builder
	for _, l := range docLines(discType, "", fmt.Sprintf("is the %q discriminator of %s. Only the values herddash generates are listed.", disc, name)) {
		fmt.Fprintf(&consts, "// %s\n", l)
	}
	fmt.Fprintf(&consts, "type %s string\n\n// %s values.\nconst (\n", discType, discType)

	sorted := append([]string(nil), variants...)
	sort.Strings(sorted)
	for _, value := range sorted {
		if forbidden[value] {
			return fmt.Errorf("%s: refusing to generate forbidden variant %q", name, value)
		}
		v, ok := byValue[value]
		if !ok {
			return fmt.Errorf("%s: schema has no variant %q", name, value)
		}
		fmt.Fprintf(&consts, "\t%s%s %s = %q\n", discType, pascal(value), discType, value)

		// The parent's own properties (a request's id) belong to every variant.
		props := map[string]*schema{}
		var required []string
		for k, p := range s.Properties {
			props[k] = p
		}
		required = append(required, s.Required...)
		for k, p := range v.Properties {
			props[k] = p
		}
		required = append(required, v.Required...)

		vname := name + pascal(value)
		doc := docLines(vname, v.Description, fmt.Sprintf("is the variant of %s whose %s is %q.", name, disc, value))
		decl, err := g.renderStruct(section, vname, doc, props, required, disc, discType)
		if err != nil {
			return err
		}
		if err := g.put(vname, decl); err != nil {
			return err
		}
	}
	consts.WriteString(")\n")
	return g.put(discType, consts.String())
}

// discriminator finds the one property every variant fixes with const.
func discriminator(variants []*schema) (string, error) {
	var found string
	for name := range variants[0].Properties {
		all := true
		for _, v := range variants {
			p, ok := v.Properties[name]
			if !ok || len(p.Const) == 0 {
				all = false
				break
			}
		}
		if all {
			if found != "" {
				return "", errors.New("more than one discriminator property")
			}
			found = name
		}
	}
	if found == "" {
		return "", errors.New("no discriminator property")
	}
	return found, nil
}

func (g *generator) renderStruct(section, name string, doc []string, props map[string]*schema, required []string, disc, discType string) (string, error) {
	req := map[string]bool{}
	for _, r := range required {
		req[r] = true
	}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, l := range doc {
		fmt.Fprintf(&b, "// %s\n", l)
	}
	fmt.Fprintf(&b, "type %s struct {\n", name)
	for _, k := range keys {
		p := props[k]
		var goType string
		nullable := false
		if k == disc {
			goType = discType
		} else {
			var err error
			goType, nullable, err = g.goType(section, name+pascal(k), p)
			if err != nil {
				return "", fmt.Errorf("%s.%s: %w", name, k, err)
			}
		}
		tag := k
		optional := !req[k] || nullable
		if optional {
			if !nilable(goType) {
				goType = "*" + goType
			}
			if !req[k] {
				tag += ",omitempty"
			}
		}
		if p.Description != "" {
			for _, line := range strings.Split(strings.TrimSpace(p.Description), "\n") {
				fmt.Fprintf(&b, "\t// %s\n", strings.TrimSpace(line))
			}
		}
		fmt.Fprintf(&b, "\t%s %s `json:%q`\n", pascal(k), goType, tag)
	}
	b.WriteString("}\n")
	return b.String(), nil
}

func nilable(goType string) bool {
	return strings.HasPrefix(goType, "[]") || strings.HasPrefix(goType, "map[") || goType == "json.RawMessage"
}

// goType returns the Go type for a property schema and whether it is nullable.
func (g *generator) goType(section, ctx string, s *schema) (string, bool, error) {
	if s.boolean {
		return "json.RawMessage", false, nil
	}
	if s.Ref != "" {
		d, err := parseRef(s.Ref)
		if err != nil {
			return "", false, err
		}
		target, err := g.lookup(d)
		if err != nil {
			return "", false, err
		}
		if len(target.OneOf) > 0 || len(target.AnyOf) > 0 {
			return "json.RawMessage", false, nil // a union, decoded later
		}
		g.need(d)
		return d.name, false, nil
	}

	if alts := append(append([]*schema(nil), s.OneOf...), s.AnyOf...); len(alts) > 0 {
		var nonNull []*schema
		for _, a := range alts {
			if t, _ := typeList(a.Type); len(t) == 1 && t[0] == "null" && a.Ref == "" {
				continue
			}
			nonNull = append(nonNull, a)
		}
		if len(nonNull) == 1 && len(alts) == 2 {
			t, _, err := g.goType(section, ctx, nonNull[0])
			return t, true, err
		}
		return "json.RawMessage", false, nil
	}

	types, err := typeList(s.Type)
	if err != nil {
		return "", false, err
	}
	nullable := false
	var rest []string
	for _, t := range types {
		if t == "null" {
			nullable = true
		} else {
			rest = append(rest, t)
		}
	}
	if len(rest) != 1 {
		return "", false, fmt.Errorf("unsupported type %v", types)
	}

	switch rest[0] {
	case "string":
		if len(s.Enum) > 0 {
			if err := g.emitEnum(ctx, s); err != nil {
				return "", false, err
			}
			return ctx, nullable, nil
		}
		return "string", nullable, nil
	case "boolean":
		return "bool", nullable, nil
	case "integer":
		switch s.Format {
		case "uint64", "uint":
			return "uint64", nullable, nil
		case "uint32":
			return "uint32", nullable, nil
		case "uint16":
			return "uint16", nullable, nil
		case "uint8":
			return "uint8", nullable, nil
		case "int64", "":
			return "int64", nullable, nil
		case "int32":
			return "int32", nullable, nil
		}
		return "", false, fmt.Errorf("unsupported integer format %q", s.Format)
	case "number":
		if s.Format == "float" {
			return "float32", nullable, nil
		}
		return "float64", nullable, nil
	case "array":
		if s.Items == nil {
			return "[]json.RawMessage", nullable, nil
		}
		t, _, err := g.goType(section, ctx+"Item", s.Items)
		return "[]" + t, nullable, err
	case "object":
		if len(s.Properties) > 0 {
			decl, err := g.renderStruct(section, ctx, docLines(ctx, s.Description, generatedFrom), s.Properties, s.Required, "", "")
			if err != nil {
				return "", false, err
			}
			return ctx, nullable, g.put(ctx, decl)
		}
		if len(s.AdditionalProperties) > 0 && string(s.AdditionalProperties) != "true" && string(s.AdditionalProperties) != "false" {
			var ap schema
			if err := json.Unmarshal(s.AdditionalProperties, &ap); err != nil {
				return "", false, err
			}
			t, _, err := g.goType(section, ctx+"Value", &ap)
			return "map[string]" + t, nullable, err
		}
		return "json.RawMessage", nullable, nil
	}
	return "", false, fmt.Errorf("unsupported type %q", rest[0])
}

// typeList reads "type", which is a string or an array of strings.
func typeList(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		return []string{one}, nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return nil, fmt.Errorf("unsupported type %s", raw)
	}
	return many, nil
}

const generatedFrom = "is generated from herdr's API schema."

// docLines is a declaration's doc comment: the fallback sentence after its name,
// followed by herdr's own description when the schema has one.
func docLines(name, description, fallback string) []string {
	lines := []string{name + " " + fallback}
	if description = strings.TrimSpace(description); description != "" {
		for _, l := range strings.Split(description, "\n") {
			lines = append(lines, strings.TrimSpace(l))
		}
	}
	return lines
}

// initialisms are rendered upper-case, as Go style asks.
var initialisms = map[string]string{"id": "ID", "ids": "IDs", "pid": "PID", "tty": "TTY", "cwd": "CWD", "url": "URL", "api": "API", "json": "JSON", "ssh": "SSH"}

// pascal turns snake_case, dotted and hyphenated names into an exported Go name.
func pascal(s string) string {
	var b strings.Builder
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == '.' || r == '-' }) {
		if up, ok := initialisms[part]; ok {
			b.WriteString(up)
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return b.String()
}
