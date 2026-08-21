package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	mdmProfileRemotePath  = "Desktop/mdm_enroll.mobileconfig"
	mdmProfileDisplayPath = "~/Desktop/mdm_enroll.mobileconfig"
	mdmPayloadIdentifier  = "397f22e4-4bd2-4533-a5ac-af41ff444b87"
)

type mdmProfileInput struct {
	BaseURL        string
	InvitationCode string
}

const mdmProfileTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>PayloadContent</key><dict>
<key>URL</key><string>%s</string>
<key>Challenge</key><string>%s</string>
<key>DeviceAttributes</key><array>
<string>UDID</string>
<string>PRODUCT</string>
<string>SERIAL</string>
<string>VERSION</string>
<string>DEVICE_NAME</string>
<string>COMPROMISED</string>
</array></dict>
<key>PayloadDescription</key><string>MDM Profile for mobile device management</string>
<key>PayloadDisplayName</key><string>MDM Profile</string>
<key>PayloadType</key><string>Profile Service</string>
<key>PayloadIdentifier</key><string>397f22e4-4bd2-4533-a5ac-af41ff444b87</string>
<key>PayloadUUID</key><string>%s</string>
<key>PayloadVersion</key><integer>1</integer>
<key>PayloadOrganization</key><string>JAMF Software</string>
</dict></plist>
`

func generateMDMProfile(input mdmProfileInput, random io.Reader) ([]byte, string, error) {
	if input.BaseURL == "" {
		return nil, "", errors.New("Jamf Pro base URL is required")
	}
	if input.InvitationCode == "" {
		return nil, "", errors.New("Jamf invitation code is required")
	}

	payloadUUID, err := newRandomUUID(random)
	if err != nil {
		return nil, "", fmt.Errorf("generate profile UUID: %w", err)
	}
	profile := []byte(fmt.Sprintf(
		mdmProfileTemplate,
		escapeMDMProfileText(input.BaseURL+"/enroll/profile"),
		escapeMDMProfileText(input.InvitationCode),
		escapeMDMProfileText(payloadUUID),
	))
	if err := validateMDMProfile(profile, input, payloadUUID); err != nil {
		return nil, "", fmt.Errorf("validate generated profile: %w", err)
	}
	return profile, payloadUUID, nil
}

func newRandomUUID(random io.Reader) (string, error) {
	value := make([]byte, 16)
	if _, err := io.ReadFull(random, value); err != nil {
		return "", err
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[:4], value[4:6], value[6:8], value[8:10], value[10:]), nil
}

func escapeMDMProfileText(value string) string {
	var escaped bytes.Buffer
	if err := xml.EscapeText(&escaped, []byte(value)); err != nil {
		panic(err)
	}
	return escaped.String()
}

func validateMDMProfile(profile []byte, input mdmProfileInput, payloadUUID string) error {
	if input.BaseURL == "" || input.InvitationCode == "" || payloadUUID == "" {
		return errors.New("missing profile validation input")
	}
	values, attributes, err := parseMDMProfile(profile)
	if err != nil {
		return err
	}
	expected := map[string]string{
		"URL":                 input.BaseURL + "/enroll/profile",
		"Challenge":           input.InvitationCode,
		"PayloadDescription":  "MDM Profile for mobile device management",
		"PayloadDisplayName":  "MDM Profile",
		"PayloadType":         "Profile Service",
		"PayloadIdentifier":   mdmPayloadIdentifier,
		"PayloadUUID":         payloadUUID,
		"PayloadVersion":      "1",
		"PayloadOrganization": "JAMF Software",
	}
	for key, want := range expected {
		if got, ok := values[key]; !ok || got != want {
			return fmt.Errorf("profile %s does not match expected value", key)
		}
	}

	expectedAttributes := []string{"UDID", "PRODUCT", "SERIAL", "VERSION", "DEVICE_NAME", "COMPROMISED"}
	if len(attributes) != len(expectedAttributes) {
		return errors.New("profile device attributes do not match")
	}
	for index, want := range expectedAttributes {
		if attributes[index] != want {
			return errors.New("profile device attributes do not match")
		}
	}
	return nil
}

type mdmProfileElement struct {
	name      xml.Name
	text      strings.Builder
	sourceKey string
}

func parseMDMProfile(profile []byte) (map[string]string, []string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(profile))
	values := make(map[string]string)
	var attributes []string
	var stack []*mdmProfileElement
	pendingKey := ""
	deviceAttributesDepth := 0

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("parse profile XML: %w", err)
		}

		switch token := token.(type) {
		case xml.StartElement:
			element := &mdmProfileElement{name: token.Name}
			if token.Name.Local == "string" || token.Name.Local == "integer" {
				element.sourceKey = pendingKey
				pendingKey = ""
			}
			stack = append(stack, element)
			if token.Name.Local == "array" {
				if pendingKey == "DeviceAttributes" {
					deviceAttributesDepth = len(stack)
				}
				pendingKey = ""
			} else if token.Name.Local == "dict" {
				pendingKey = ""
			}
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text.Write([]byte(token))
			}
		case xml.EndElement:
			if len(stack) == 0 || stack[len(stack)-1].name != token.Name {
				return nil, nil, errors.New("profile XML has mismatched elements")
			}
			element := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			switch element.name.Local {
			case "key":
				pendingKey = element.text.String()
			case "string", "integer":
				if deviceAttributesDepth > 0 && element.name.Local == "string" && len(stack) == deviceAttributesDepth {
					attributes = append(attributes, element.text.String())
				}
				if element.sourceKey != "" {
					if _, exists := values[element.sourceKey]; exists {
						return nil, nil, fmt.Errorf("profile contains duplicate %s", element.sourceKey)
					}
					values[element.sourceKey] = element.text.String()
				}
			case "array":
				if deviceAttributesDepth == len(stack)+1 {
					deviceAttributesDepth = 0
				}
			}
		}
	}
	if len(stack) != 0 {
		return nil, nil, errors.New("profile XML has unclosed elements")
	}
	return values, attributes, nil
}
