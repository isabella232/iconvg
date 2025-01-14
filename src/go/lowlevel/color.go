// Copyright 2021 The IconVG Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package lowlevel

import (
	"image/color"
)

func validAlphaPremulColor(c color.RGBA) bool {
	return c.R <= c.A && c.G <= c.A && c.B <= c.A
}

// colorType distinguishes types of Colors.
type colorType uint8

const (
	// colorTypeRGBA is a direct RGBA color.
	colorTypeRGBA colorType = iota

	// colorTypePaletteIndex is an indirect color, indexing the custom palette.
	colorTypePaletteIndex

	// colorTypeCReg is an indirect color, indexing the CREG color registers.
	colorTypeCReg

	// colorTypeBlend is an indirect color, blending two other colors.
	colorTypeBlend
)

// Color is an IconVG color, whose RGBA values can depend on context. Some
// Colors are direct RGBA values. Other Colors are indirect, referring to an
// index of the custom palette, a color register of the decoder virtual
// machine, or a blend of two other Colors.
//
// See the "Colors" section in the specification for details.
type Color struct {
	typ  colorType
	data color.RGBA
}

func (c Color) rgba() color.RGBA         { return c.data }
func (c Color) paletteIndex() uint8      { return c.data.R }
func (c Color) cReg() uint8              { return c.data.R }
func (c Color) blend() (t, c0, c1 uint8) { return c.data.R, c.data.G, c.data.B }

// Resolve resolves the Color's RGBA value, given its context: the custom
// palette and the color registers of the decoder virtual machine.
func (c Color) Resolve(pal *Palette, cReg *[64]color.RGBA) color.RGBA {
	switch c.typ {
	case colorTypeRGBA:
		return c.rgba()
	case colorTypePaletteIndex:
		return pal[c.paletteIndex()&0x3f]
	case colorTypeCReg:
		return cReg[c.cReg()&0x3f]
	}
	t, c0, c1 := c.blend()
	p, q := uint32(255-t), uint32(t)
	rgba0 := decodeColor1(c0).Resolve(pal, cReg)
	rgba1 := decodeColor1(c1).Resolve(pal, cReg)
	return color.RGBA{
		uint8(((p * uint32(rgba0.R)) + q*uint32(rgba1.R) + 128) / 255),
		uint8(((p * uint32(rgba0.G)) + q*uint32(rgba1.G) + 128) / 255),
		uint8(((p * uint32(rgba0.B)) + q*uint32(rgba1.B) + 128) / 255),
		uint8(((p * uint32(rgba0.A)) + q*uint32(rgba1.A) + 128) / 255),
	}
}

// RGBAColor returns a direct Color.
func RGBAColor(c color.RGBA) Color { return Color{colorTypeRGBA, c} }

// PaletteIndexColor returns an indirect Color referring to an index of the
// custom palette.
func PaletteIndexColor(i uint8) Color { return Color{colorTypePaletteIndex, color.RGBA{R: i & 0x3f}} }

// CRegColor returns an indirect Color referring to a color register of the
// decoder virtual machine.
func CRegColor(i uint8) Color { return Color{colorTypeCReg, color.RGBA{R: i & 0x3f}} }

// BlendColor returns an indirect Color that blends two other Colors. Those two
// other Colors must both be encodable as a 1 byte color.
//
// To blend a Color that is not encodable as a 1 byte color, first load that
// Color into a CREG color register, then call CRegColor to produce a Color
// that is encodable as a 1 byte color. See testdata/favicon.ivg for an
// example.
//
// See the "Colors" section in the specification for details.
func BlendColor(t, c0, c1 uint8) Color { return Color{colorTypeBlend, color.RGBA{R: t, G: c0, B: c1}} }

func decodeColor1(x byte) Color {
	if x >= 0x80 {
		if x >= 0xc0 {
			return CRegColor(x)
		} else {
			return PaletteIndexColor(x)
		}
	}
	if x >= 125 {
		switch x - 125 {
		case 0:
			return RGBAColor(color.RGBA{0xc0, 0xc0, 0xc0, 0xc0})
		case 1:
			return RGBAColor(color.RGBA{0x80, 0x80, 0x80, 0x80})
		case 2:
			return RGBAColor(color.RGBA{0x00, 0x00, 0x00, 0x00})
		}
	}
	blue := dc1Table[x%5]
	x = x / 5
	green := dc1Table[x%5]
	x = x / 5
	red := dc1Table[x]
	return RGBAColor(color.RGBA{red, green, blue, 0xff})
}

var dc1Table = [5]byte{0x00, 0x40, 0x80, 0xc0, 0xff}

func is1(u uint8) bool { return u&0x3f == 0 || u == 0xff }

func encodeColor1(c Color) (x byte, ok bool) {
	switch c.typ {
	case colorTypeRGBA:
		if c.data.A != 0xff {
			switch c.data {
			case color.RGBA{0x00, 0x00, 0x00, 0x00}:
				return 127, true
			case color.RGBA{0x80, 0x80, 0x80, 0x80}:
				return 126, true
			case color.RGBA{0xc0, 0xc0, 0xc0, 0xc0}:
				return 125, true
			}
		} else if is1(c.data.R) && is1(c.data.G) && is1(c.data.B) && is1(c.data.A) {
			r := c.data.R / 0x3f
			g := c.data.G / 0x3f
			b := c.data.B / 0x3f
			return 25*r + 5*g + b, true
		}
	case colorTypePaletteIndex:
		return c.data.R | 0x80, true
	case colorTypeCReg:
		return c.data.R | 0xc0, true
	}
	return 0, false
}

func is2(u uint8) bool { return u%0x11 == 0 }

func encodeColor2(c Color) (x [2]byte, ok bool) {
	if c.typ == colorTypeRGBA && is2(c.data.R) && is2(c.data.G) && is2(c.data.B) && is2(c.data.A) {
		return [2]byte{
			(c.data.R/0x11)<<4 | (c.data.G / 0x11),
			(c.data.B/0x11)<<4 | (c.data.A / 0x11),
		}, true
	}
	return [2]byte{}, false
}

func encodeColor3Direct(c Color) (x [3]byte, ok bool) {
	if c.typ == colorTypeRGBA && c.data.A == 0xff {
		return [3]byte{c.data.R, c.data.G, c.data.B}, true
	}
	return [3]byte{}, false
}

func encodeColor4(c Color) (x [4]byte, ok bool) {
	if c.typ == colorTypeRGBA {
		return [4]byte{c.data.R, c.data.G, c.data.B, c.data.A}, true
	}
	return [4]byte{}, false
}

func encodeColor3Indirect(c Color) (x [3]byte, ok bool) {
	if c.typ == colorTypeBlend {
		return [3]byte{c.data.R, c.data.G, c.data.B}, true
	}
	return [3]byte{}, false
}
