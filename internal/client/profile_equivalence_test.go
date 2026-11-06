// SPDX-License-Identifier: MPL-2.0

package client

import (
	"reflect"
	"strings"
	"testing"
)

const sourcePayload = `<plist version="1.0"><dict><key>PayloadType</key><string>Configuration</string><key>PayloadVersion</key><integer>1</integer><key>PayloadUUID</key><string>source-uuid</string><key>PayloadIdentifier</key><string>source-identifier</string><key>PayloadDisplayName</key><string>Source name</string><key>PayloadContent</key><array><dict><key>PayloadIdentifier</key><string>nested-identifier</string><key>Setting</key><string> exact value </string></dict></array></dict></plist>`

func normalizedProfileFixture() Profile {
	return Profile{ID: testID, MDMIdentifier: "com.kandji.profile.custom." + testID, ProfileInput: ProfileInput{Name: "API name", Profile: `<?xml version="1.0"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict>
<key>PayloadContent</key><array><dict><key>Setting</key><string> exact value </string><key>PayloadIdentifier</key><string>nested-identifier</string></dict></array>
<key>PayloadDisplayName</key><string>API name</string><key>PayloadIdentifier</key><string>com.kandji.profile.custom.` + testID + `</string><key>PayloadScope</key><string>System</string><key>PayloadType</key><string>Configuration</string><key>PayloadUUID</key><string>` + testID + `</string><key>PayloadVersion</key><integer>1</integer></dict></plist>`}}
}

func TestProfileEquivalenceVerifiesGeneratedEnvelopeAndRetainsManagedContent(t *testing.T) {
	p := normalizedProfileFixture()
	if !ProfileSourceEquivalent(sourcePayload, p) {
		t.Fatal("verified generated envelope/formatting did not compare equal")
	}
	for _, change := range [][2]string{{"nested-identifier", "different-nested-id"}, {" exact value ", "exact value"}, {"<string>System</string>", "<string>User</string>"}, {"<integer>1</integer>", "<integer>2</integer>"}, {"<string>API name</string>", "<string>wrong name</string>"}, {"<string>com.kandji.profile.custom." + testID + "</string>", "<string>wrong identifier</string>"}, {"<string>" + testID + "</string>", "<string>invalid-generated-uuid</string>"}} {
		changed := p
		changed.Profile = strings.ReplaceAll(p.Profile, change[0], change[1])
		if ProfileSourceEquivalent(sourcePayload, changed) {
			t.Fatalf("masked meaningful change in %s", change[0])
		}
	}
	for _, extra := range []string{`<key>PayloadScope</key><string>User</string>`, `<key>PayloadRemovalDisallowed</key><true/>`, `<key>OtherSetting</key><string>retained</string>`} {
		source := strings.Replace(sourcePayload, "<dict>", "<dict>"+extra, 1)
		if ProfileSourceEquivalent(source, p) {
			t.Fatal("masked source scope or policy removal")
		}
	}
}

func TestProfileComparisonRefusesAmbiguousOrUnsupportedPlists(t *testing.T) {
	for _, xml := range []string{"", `<plist><dict><key>x</key></dict></plist>`, `<plist><dict><key>x</key><true/><key>x</key><false/></dict></plist>`, `<plist><dict><key>x</key><string><string>nested</string></string></dict></plist>`, `<plist><dict/><dict/></plist>`, `<plist xmlns="private"><dict/></plist>`, `<plist><dict> </dict></plist>`, `<plist><dict/></plist><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">`, `<!DOCTYPE plist SYSTEM "file:///private"><plist><dict/></plist>`, `<plist><dict><key>x</key><key>not-value</key></dict></plist>`, strings.Repeat("<array>", 129)} {
		if _, ok := parseProfilePlist(xml); ok {
			t.Fatal("accepted ambiguous or unsupported structure")
		}
	}
	if ProfileSourceEquivalent(sourcePayload, Profile{ProfileInput: ProfileInput{Profile: sourcePayload}}) {
		t.Fatal("unverified generated metadata accepted")
	}
}

func TestProfileComparisonPreservesArrayOrderScalarTypesAndWhitespace(t *testing.T) {
	base := `<plist><dict><key>value</key><array><string>a</string><integer>1</integer></array></dict></plist>`
	a, ok := parseProfilePlist(base)
	if !ok {
		t.Fatal("fixture")
	}
	for _, other := range []string{strings.Replace(base, "<string>a</string><integer>1</integer>", "<integer>1</integer><string>a</string>", 1), strings.Replace(base, "<integer>1</integer>", "<string>1</string>", 1), strings.Replace(base, ">a<", "> a <", 1)} {
		b, ok := parseProfilePlist(other)
		if !ok || reflect.DeepEqual(a, b) {
			t.Fatal("lost array order, type, or significant whitespace")
		}
	}
}

func TestProfileGeneratedUUIDIsIndependentOfLibraryItemIdentity(t *testing.T) {
	p := normalizedProfileFixture()
	p.Profile = strings.Replace(p.Profile, "<string>"+testID+"</string>", "<string>aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee</string>", 1)
	if !ProfileSourceEquivalent(sourcePayload, p) {
		t.Fatal("independent generated payload UUID rejected")
	}
	p.ID = "invalid-item-id"
	if ProfileSourceEquivalent(sourcePayload, p) {
		t.Fatal("invalid Library Item identity accepted")
	}
}

func TestGeneratedRootUUIDDoesNotMaskNestedPayloadUUIDDrift(t *testing.T) {
	p := normalizedProfileFixture()
	pair := `<key>PayloadUUID</key><string>11111111-2222-4333-8444-555555555555</string>`
	source := strings.Replace(sourcePayload, `<array><dict>`, `<array><dict>`+pair, 1)
	p.Profile = strings.Replace(p.Profile, `<array><dict>`, `<array><dict>`+pair, 1)
	if !ProfileSourceEquivalent(source, p) {
		t.Fatal("matching nested UUID rejected")
	}
	p.Profile = strings.Replace(p.Profile, pair, `<key>PayloadUUID</key><string>aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee</string>`, 1)
	if ProfileSourceEquivalent(source, p) {
		t.Fatal("nested payload UUID drift hidden")
	}
}
