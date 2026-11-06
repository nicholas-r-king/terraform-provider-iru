// SPDX-License-Identifier: MPL-2.0

package client

import (
	"encoding/xml"
	"io"
	"reflect"
	"strings"
)

// ProfileSourceEquivalent compares managed payloads while verifying the three
// top-level metadata fields Iru generates from the Library Item. Nested payload
// identifiers, scope changes, policy values and unknown root keys are not ignored.
// Live API reads show the generated payload UUID is independent of the item ID.
// Item identity is checked by the client; payload identity is server-generated.
func ProfileSourceEquivalent(source string, remote Profile) bool {
	a, ok := parseProfilePlist(source)
	if !ok {
		return false
	}
	b, ok := parseProfilePlist(remote.Profile)
	if !ok {
		return false
	}
	text := func(d map[string]plistValue, key string) (string, bool) {
		v, ok := d[key]
		return v.Text, ok && v.Kind == "string"
	}
	uuid, ok := text(b.Dict, "PayloadUUID")
	if !ok || !ValidID(uuid) || !ValidID(remote.ID) {
		return false
	}
	identifier, ok := text(b.Dict, "PayloadIdentifier")
	if !ok || identifier != remote.MDMIdentifier || identifier == "" {
		return false
	}
	name, ok := text(b.Dict, "PayloadDisplayName")
	if !ok || name != remote.Name {
		return false
	}
	for _, key := range []string{"PayloadUUID", "PayloadIdentifier", "PayloadDisplayName"} {
		if v, exists := a.Dict[key]; exists && v.Kind != "string" {
			return false
		}
		delete(a.Dict, key)
		delete(b.Dict, key)
	}
	// Only the observed default System scope can be added when the source omits
	// it. An explicit User scope must never silently compare equal to System.
	if _, exists := a.Dict["PayloadScope"]; !exists {
		if scope, exists := b.Dict["PayloadScope"]; exists {
			if scope.Kind != "string" || scope.Text != "System" {
				return false
			}
			delete(b.Dict, "PayloadScope")
		}
	}
	return reflect.DeepEqual(a, b)
}

type plistValue struct {
	Kind  string
	Text  string
	Dict  map[string]plistValue
	Items []plistValue
}
type plistXMLNode struct {
	name     string
	text     strings.Builder
	children []*plistXMLNode
}

// A bounded, non-network XML plist comparison parser. Keep scalar text exact;
// only whitespace between container elements and dictionary order are formatting.
// Unsupported encodings/structures fail comparison rather than mask drift.
func parseProfilePlist(content string) (plistValue, bool) {
	bad := plistValue{}
	if len(content) == 0 || len(content) > responseLimit {
		return bad, false
	}
	d := xml.NewDecoder(strings.NewReader(content))
	var stack []*plistXMLNode
	var root *plistXMLNode
	count := 0
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return bad, false
		}
		count++
		if count > 100000 {
			return bad, false
		}
		switch t := token.(type) {
		case xml.StartElement:
			if len(stack) >= 128 || t.Name.Space != "" {
				return bad, false
			}
			for _, attr := range t.Attr {
				if t.Name.Local != "plist" || attr.Name.Space != "" || attr.Name.Local != "version" || attr.Value != "1.0" {
					return bad, false
				}
			}
			if len(t.Attr) > 1 {
				return bad, false
			}
			n := &plistXMLNode{name: t.Name.Local}
			if len(stack) == 0 {
				if root != nil {
					return bad, false
				}
				root = n
			} else {
				p := stack[len(stack)-1]
				p.children = append(p.children, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) == 0 {
				return bad, false
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 {
				if strings.Trim(string(t), " \t\r\n") != "" {
					return bad, false
				}
			} else {
				stack[len(stack)-1].text.Write(t)
			}
		case xml.Directive:
			const appleDTD = `DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"`
			if root != nil || len(stack) != 0 || strings.Join(strings.Fields(string(t)), " ") != appleDTD {
				return bad, false
			}
		case xml.ProcInst:
			if t.Target != "xml" || len(stack) != 0 || root != nil {
				return bad, false
			}
		case xml.Comment:
		default:
			return bad, false
		}
	}
	if len(stack) != 0 || root == nil || root.name != "plist" || strings.Trim(root.text.String(), " \t\r\n") != "" || len(root.children) != 1 || root.children[0].name != "dict" {
		return bad, false
	}
	return plistNodeValue(root.children[0])
}

func plistNodeValue(n *plistXMLNode) (plistValue, bool) {
	v := plistValue{Kind: n.name}
	switch n.name {
	case "dict":
		if strings.Trim(n.text.String(), " \t\r\n") != "" || len(n.children)%2 != 0 {
			return v, false
		}
		v.Dict = map[string]plistValue{}
		for i := 0; i < len(n.children); i += 2 {
			k := n.children[i]
			if k.name != "key" || len(k.children) != 0 {
				return v, false
			}
			if _, exists := v.Dict[k.text.String()]; exists {
				return v, false
			}
			value, ok := plistNodeValue(n.children[i+1])
			if !ok {
				return v, false
			}
			v.Dict[k.text.String()] = value
		}
	case "array":
		if strings.Trim(n.text.String(), " \t\r\n") != "" {
			return v, false
		}
		v.Items = []plistValue{}
		for _, child := range n.children {
			value, ok := plistNodeValue(child)
			if !ok {
				return v, false
			}
			v.Items = append(v.Items, value)
		}
	case "string", "integer", "real", "date", "data":
		if len(n.children) != 0 {
			return v, false
		}
		v.Text = n.text.String()
	case "true", "false":
		if len(n.children) != 0 || strings.Trim(n.text.String(), " \t\r\n") != "" {
			return v, false
		}
	default:
		return v, false
	}
	return v, true
}
