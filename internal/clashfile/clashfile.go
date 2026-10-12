// Package clashfile reads the part of a Clash-format YAML file a
// core.Profile models: proxies, proxy groups, rules and inline rule
// providers. It lets a development daemon run a real subscription before the
// daemon downloads and allowlists its own; it allowlists nothing, so only
// root's own files belong in it.
package clashfile

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/readeem/omihomo/internal/core"
	"github.com/readeem/omihomo/internal/protocol"
)

type file struct {
	Proxies       []map[string]any          `yaml:"proxies"`
	Groups        []map[string]any          `yaml:"proxy-groups"`
	Rules         []string                  `yaml:"rules"`
	RuleProviders map[string]map[string]any `yaml:"rule-providers"`
}

// Parse reads data into a profile whose rules are all the subscription's.
// What the profile cannot carry is left out and named in skipped: proxy
// providers, which a group's "use" names, and rule providers that are not
// inline, along with the RULE-SET rules that match against them.
func Parse(data []byte) (p core.Profile, skipped []string, err error) {
	var f file
	if err := yaml.Unmarshal(data, &f); err != nil {
		return core.Profile{}, nil, fmt.Errorf("read the YAML: %w", err)
	}
	for i, m := range f.Proxies {
		name, typ, err := nameAndType(m)
		if err != nil {
			return core.Profile{}, nil, fmt.Errorf("proxy %d: %w", i+1, err)
		}
		opts := maps.Clone(m)
		delete(opts, "name")
		delete(opts, "type")
		p.Proxies = append(p.Proxies, core.Proxy{Name: name, Type: typ, Options: opts})
	}
	for i, m := range f.Groups {
		name, typ, err := nameAndType(m)
		if err != nil {
			return core.Profile{}, nil, fmt.Errorf("proxy group %d: %w", i+1, err)
		}
		opts := maps.Clone(m)
		var members []string
		if raw, ok := opts["proxies"].([]any); ok {
			for _, v := range raw {
				s, ok := v.(string)
				if !ok {
					return core.Profile{}, nil, fmt.Errorf("proxy group %q: a member is not a name", name)
				}
				members = append(members, s)
			}
		}
		if _, ok := opts["use"]; ok {
			skipped = append(skipped, fmt.Sprintf("the proxy providers group %q uses", name))
		}
		for _, k := range []string{"name", "type", "proxies", "use"} {
			delete(opts, k)
		}
		p.Groups = append(p.Groups, core.Group{Name: name, Type: typ, Members: members, Options: opts})
	}

	inline := map[string]bool{}
	for _, name := range slices.Sorted(maps.Keys(f.RuleProviders)) {
		m := f.RuleProviders[name]
		behavior, _ := m["behavior"].(string)
		raw, isList := m["payload"].([]any)
		if m["type"] != "inline" || !isList {
			skipped = append(skipped, fmt.Sprintf("rule provider %q, which is not inline", name))
			continue
		}
		set := core.RuleSet{Name: name, Behavior: behavior}
		for _, v := range raw {
			s, ok := v.(string)
			if !ok {
				return core.Profile{}, nil, fmt.Errorf("rule provider %q: a payload line is not a string", name)
			}
			set.Payload = append(set.Payload, s)
		}
		inline[name] = true
		p.RuleSets = append(p.RuleSets, set)
	}

	for _, line := range f.Rules {
		r, err := ParseRule(line)
		if err != nil {
			return core.Profile{}, nil, err
		}
		if r.Type == "RULE-SET" && !inline[r.Value] {
			skipped = append(skipped, fmt.Sprintf("rule %q", line))
			continue
		}
		r.Source = protocol.SourceSubscription
		p.Rules = append(p.Rules, r)
	}
	return p, skipped, nil
}

func nameAndType(m map[string]any) (name, typ string, err error) {
	name, _ = m["name"].(string)
	typ, _ = m["type"].(string)
	if name == "" || typ == "" {
		return "", "", fmt.Errorf("no name or type")
	}
	return name, typ, nil
}

// ParseRule splits a rule line such as "DOMAIN-SUFFIX,example.com,Proxy" or
// "AND,((DOMAIN,a.com),(NETWORK,UDP)),REJECT". Source is left empty.
func ParseRule(line string) (core.Rule, error) {
	typ, rest, ok := strings.Cut(strings.TrimSpace(line), ",")
	if !ok || typ == "" || rest == "" {
		return core.Rule{}, fmt.Errorf("rule %q has no target", line)
	}
	typ = strings.ToUpper(typ)
	if typ == "MATCH" {
		parts := strings.Split(rest, ",")
		return core.Rule{Type: typ, Target: parts[0], Params: parts[1:]}, nil
	}
	var value string
	if strings.HasPrefix(rest, "(") {
		// A logic rule's value is one parenthesised list with commas inside.
		depth, end := 0, -1
		for i, r := range rest {
			switch r {
			case '(':
				depth++
			case ')':
				depth--
			}
			if depth == 0 {
				end = i
				break
			}
		}
		if end < 0 || end+1 >= len(rest) || rest[end+1] != ',' {
			return core.Rule{}, fmt.Errorf("rule %q has unbalanced parentheses", line)
		}
		value, rest = rest[:end+1], rest[end+2:]
	} else {
		value, rest, ok = strings.Cut(rest, ",")
		if !ok {
			return core.Rule{}, fmt.Errorf("rule %q has no target", line)
		}
	}
	parts := strings.Split(rest, ",")
	if parts[0] == "" {
		return core.Rule{}, fmt.Errorf("rule %q has no target", line)
	}
	return core.Rule{Type: typ, Value: value, Target: parts[0], Params: parts[1:]}, nil
}
