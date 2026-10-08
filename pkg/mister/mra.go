package mister

import (
	"encoding/xml"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// MRA represents the top-level <misterromdescription> element of an MRA file.
type MRA struct {
	XMLName  xml.Name      `xml:"misterromdescription"`
	Name     string        `xml:"name"`
	RBF      string        `xml:"rbf"`
	SetName  string        `xml:"setname"`
	Switches []MRASwitches `xml:"switches"`
}

// MRASwitches represents the <switches> element containing DIP switch definitions.
type MRASwitches struct {
	Default string   `xml:"default,attr"`
	Base    string   `xml:"base,attr"`
	Dips    []MRADip `xml:"dip"`
}

// MRADip represents a single <dip> element defining one DIP switch.
type MRADip struct {
	Bits string `xml:"bits,attr"`
	Name string `xml:"name,attr"`
	IDs  string `xml:"ids,attr"`
}

// DIPSwitch represents a parsed DIP switch with its bit position and possible values.
type DIPSwitch struct {
	Name    string   `json:"name"`
	Bit     int      `json:"bit"`
	BitHigh int      `json:"bit_high"`
	Values  []string `json:"values"`
}

// ParseMRA parses an MRA XML file and returns the parsed structure.
func ParseMRA(path string) (*MRA, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading MRA %s: %w", path, err)
	}
	return ParseMRAData(data)
}

// ParseMRAData parses MRA XML data.
func ParseMRAData(data []byte) (*MRA, error) {
	var mra MRA
	if err := xml.Unmarshal(data, &mra); err != nil {
		return nil, fmt.Errorf("parsing MRA XML: %w", err)
	}
	return &mra, nil
}

// ParseDIPSwitches extracts DIP switch definitions from an MRA.
// The <dip> tag format: bits="10" name="Free Play" ids="On,Off"
// means starting at bit 10, values are On(index 0) and Off(index 1).
// For multi-bit DIPs like bits="8,9" or bits="8-9", a range is used.
func ParseDIPSwitches(mra *MRA) []DIPSwitch {
	var dips []DIPSwitch
	for _, sw := range mra.Switches {
		for _, d := range sw.Dips {
			ds := parseDIPSwitch(d)
			if ds != nil {
				dips = append(dips, *ds)
			}
		}
	}
	return dips
}

// parseDIPSwitch parses a single MRA <dip> element into a DIPSwitch.
func parseDIPSwitch(d MRADip) *DIPSwitch {
	if d.Name == "" || d.Bits == "" {
		return nil
	}

	low, high := parseDIPBits(d.Bits)
	values := parseDIPIDs(d.IDs)

	return &DIPSwitch{
		Name:    d.Name,
		Bit:     low,
		BitHigh: high,
		Values:  values,
	}
}

// parseDIPBits parses the bits attribute of a <dip> element.
// Formats: "10" (single bit), "8,9" (comma-separated), "8-9" (range).
func parseDIPBits(s string) (int, int) {
	s = strings.TrimSpace(s)

	// Comma-separated: "8,9"
	if strings.Contains(s, ",") {
		parts := strings.Split(s, ",")
		bits := make([]int, 0, len(parts))
		for _, p := range parts {
			if b, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
				bits = append(bits, b)
			}
		}
		if len(bits) == 0 {
			return 0, 0
		}
		low, high := bits[0], bits[0]
		for _, b := range bits[1:] {
			if b < low {
				low = b
			}
			if b > high {
				high = b
			}
		}
		return low, high
	}

	// Range: "8-9"
	if strings.Contains(s, "-") {
		parts := strings.SplitN(s, "-", 2)
		low, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		high, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err1 == nil && err2 == nil {
			if low > high {
				low, high = high, low
			}
			return low, high
		}
	}

	// Single bit: "10"
	if b, err := strconv.Atoi(s); err == nil {
		return b, b
	}

	return 0, 0
}

// parseDIPIDs splits the ids attribute into individual value names.
func parseDIPIDs(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	values := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			values = append(values, p)
		}
	}
	return values
}

// FindDIPSwitch finds a DIP switch by name (case-insensitive).
func FindDIPSwitch(dips []DIPSwitch, name string) *DIPSwitch {
	target := strings.ToLower(name)
	for i := range dips {
		if strings.ToLower(dips[i].Name) == target {
			return &dips[i]
		}
	}
	return nil
}

// FindDIPValue returns the index of a value name within a DIP switch's Values list.
// Returns -1 if not found. Case-insensitive.
func FindDIPValue(dip *DIPSwitch, valueName string) int {
	target := strings.ToLower(valueName)
	for i, v := range dip.Values {
		if strings.ToLower(v) == target {
			return i
		}
	}
	return -1
}
