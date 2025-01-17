package main

import (
	"bufio"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
)

// PICO-8 16-color palette
var pico8Palette = []color.RGBA{
	{0, 0, 0, 255},       // 0: Black
	{29, 43, 83, 255},    // 1: Dark Blue
	{126, 37, 83, 255},   // 2: Dark Purple
	{0, 135, 81, 255},    // 3: Dark Green
	{171, 82, 54, 255},   // 4: Brown
	{95, 87, 79, 255},    // 5: Dark Gray
	{194, 195, 199, 255}, // 6: Light Gray
	{255, 241, 232, 255}, // 7: White
	{255, 0, 77, 255},    // 8: Red
	{255, 163, 0, 255},   // 9: Orange
	{255, 236, 39, 255},  // 10: Yellow
	{0, 228, 54, 255},    // 11: Green
	{41, 173, 255, 255},  // 12: Blue
	{131, 118, 156, 255}, // 13: Indigo
	{255, 119, 168, 255}, // 14: Pink
	{255, 204, 170, 255}, // 15: Peach
}

func main() {
	// Flags: user can specify a cart path, and optional --3 or --4
	var cartPath string
	var useSection3, useSection4 bool
	var cleanSlate bool

	flag.StringVar(&cartPath, "cart", "/Users/pgeorgia/Library/Application Support/pico-8/carts/test.p8",
		"Path to the PICO-8 cartridge file (.p8)")
	flag.BoolVar(&useSection3, "3", false, "Include dual-purpose section 3 (sprites 128..191)")
	flag.BoolVar(&useSection4, "4", false, "Include dual-purpose section 4 (sprites 192..255)")
	flag.BoolVar(&cleanSlate, "clean", false, "Remove old sprites directory, map.png, spritesheet.png if they exist")
	flag.Parse()

	// Clean up old artifacts if requested
	if cleanSlate {
		if err := os.RemoveAll("sprites"); err == nil {
			fmt.Println("Removed old sprites/ folder.")
		}
		os.Remove("map.png")
		os.Remove("spritesheet.png")
	}

	// Parse sections from the PICO-8 cart
	gfxData := parseSection(cartPath, "__gfx__")
	if len(gfxData) == 0 {
		fmt.Fprintln(os.Stderr, "No __gfx__ section found in cart. Exiting.")
		os.Exit(1)
	}
	mapData := parseSection(cartPath, "__map__")
	if len(mapData) == 0 {
		fmt.Fprintln(os.Stderr, "No __map__ section found in cart. Exiting.")
		os.Exit(1)
	}

	// Potential dual-purpose sections
	// Each sprite row is 8 pixels.
	// - Section 3 covers sprite 128..191 => rows 8..11 in the 16x16 grid
	// - Section 4 covers sprite 192..255 => rows 12..15
	var dualPurposeSection1, dualPurposeSection2 []string
	if useSection3 {
		startRow := 8 * 8       // 64
		endRow := startRow + 32 // 96
		// Clamp if gfxData is too short
		if endRow > len(gfxData) {
			endRow = len(gfxData)
		}
		if startRow < len(gfxData) {
			dualPurposeSection1 = gfxData[startRow:endRow]
		}
	}

	if useSection4 {
		startRow := 12 * 8      // 96
		endRow := startRow + 32 // 128
		if endRow > len(gfxData) {
			endRow = len(gfxData)
		}
		if startRow < len(gfxData) {
			dualPurposeSection2 = gfxData[startRow:endRow]
		}
	}

	// Create full 16x16 sprite sheet
	spriteSheet := reconstructImage(gfxData)

	// Decide map height
	mapHeight := 32
	if useSection3 {
		mapHeight = 48
	}
	if useSection4 {
		mapHeight = 64
	}

	// Render map with optional dual-purpose sections
	mapImage := renderMap(mapData, dualPurposeSection1, dualPurposeSection2, spriteSheet, 128, mapHeight)
	if err := saveAsPng(mapImage, "map.png"); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving map.png: %v\n", err)
	}

	// Save sprites (some or all) and sprite sub-sections
	saveSprites(spriteSheet, useSection3, useSection4)

	// Then combine those sub-sections into a single sprite sheet
	numSections := 4
	if useSection3 || useSection4 {
		numSections = 2
	}
	if err := combineSectionsIntoSpriteSheet(numSections); err != nil {
		fmt.Println("Error combining sections:", err)
	}
}

// parseSection reads lines between a given marker (e.g. __gfx__) until next marker __*
func parseSection(filePath, sectionName string) []string {
	f, err := os.Open(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open cart file: %v\n", err)
		return nil
	}
	defer f.Close()

	var section []string
	inSection := false

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()

		// If we encounter the marker (e.g. __gfx__), we start capturing
		if strings.HasPrefix(line, sectionName) {
			inSection = true
			continue
		}
		// If we see any other marker (e.g. __lua__, __map__, etc.) we stop
		if strings.HasPrefix(line, "__") && line != sectionName {
			inSection = false
		}

		if inSection {
			section = append(section, line)
		}
	}
	return section
}

// reconstructImage puts the 16x16 sprite data into an RGBA image
func reconstructImage(gfxData []string) *image.RGBA {
	const size = 16 * 8 // 128
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	for y, line := range gfxData {
		for x, hexChar := range line {
			colorIndex := parseHexChar(hexChar)
			if colorIndex >= 0 && colorIndex < len(pico8Palette) {
				img.Set(x, y, pico8Palette[colorIndex])
			} else {
				// If out of range or invalid hex, default to black
				img.Set(x, y, pico8Palette[0])
			}
		}
	}

	return img
}

// renderMap draws the map data (and dual-purpose sections) onto a new RGBA
func renderMap(
	mapData []string,
	dualPurposeSection1, dualPurposeSection2 []string,
	spriteSheet *image.RGBA,
	mapWidth, mapHeight int,
) *image.RGBA {
	const tileSize = 8
	mapImage := image.NewRGBA(image.Rect(0, 0, mapWidth*tileSize, mapHeight*tileSize))

	// Fill background with black
	for y := 0; y < mapHeight*tileSize; y++ {
		for x := 0; x < mapWidth*tileSize; x++ {
			mapImage.Set(x, y, pico8Palette[0])
		}
	}

	// Draw regular map data (each line is hex data for 128 tiles = 256 hex chars)
	for y := 0; y < len(mapData); y++ {
		line := mapData[y]
		// Each tile is 2 hex digits => 1 tile
		for x := 0; x < len(line)/2; x++ {
			spriteY := parseHexChar(rune(line[x*2]))   // First hex digit is spriteY
			spriteX := parseHexChar(rune(line[x*2+1])) // Second hex digit is spriteX

			if spriteX != 0 || spriteY != 0 {
				drawSprite(mapImage, spriteSheet, spriteX, spriteY, x, y)
			}
		}
	}

	// Now draw dual-purpose section 1 (section 3)
	if dualPurposeSection1 != nil {
		for y := 0; y < len(dualPurposeSection1); y++ {
			line := dualPurposeSection1[y]
			for x := 0; x < len(line)/2; x++ {
				spriteX := parseHexChar(rune(line[x*2]))
				spriteY := parseHexChar(rune(line[x*2+1]))

				yIsEven := (y % 2) == 0
				if yIsEven {
					// same logic as your original "if yIsOdd" (slight rename for clarity)
					if spriteX != 0 || spriteY != 0 {
						drawSprite(mapImage, spriteSheet, spriteX, spriteY, x, 32+(y/2))
					} else {
						drawBlackTile(mapImage, x, 32+(y/2))
					}
				} else {
					if spriteX != 0 || spriteY != 0 {
						drawSprite(mapImage, spriteSheet, spriteX, spriteY, 64+x, 32+((y-1)/2))
					}
				}
			}
		}
	}

	// Dual-purpose section 2 (section 4)
	if dualPurposeSection2 != nil {
		for y := 0; y < len(dualPurposeSection2); y++ {
			line := dualPurposeSection2[y]
			for x := 0; x < len(line)/2; x++ {
				spriteX := parseHexChar(rune(line[x*2]))
				spriteY := parseHexChar(rune(line[x*2+1]))

				yIsEven := (y % 2) == 0
				if yIsEven {
					if spriteX != 0 || spriteY != 0 {
						drawSprite(mapImage, spriteSheet, spriteX, spriteY, x, 48+(y/2))
					}
				} else {
					if spriteX != 0 || spriteY != 0 {
						drawSprite(mapImage, spriteSheet, spriteX, spriteY, 64+x, 48+((y-1)/2))
					}
				}
			}
		}
	}

	return mapImage
}

// drawSprite copies an 8x8 region from the sprite sheet
func drawSprite(dst *image.RGBA, src *image.RGBA, spriteX, spriteY, dstTileX, dstTileY int) {
	const tileSize = 8
	srcX := spriteX * tileSize
	srcY := spriteY * tileSize
	dstX := dstTileX * tileSize
	dstY := dstTileY * tileSize

	for yy := 0; yy < tileSize; yy++ {
		for xx := 0; xx < tileSize; xx++ {
			dst.Set(dstX+xx, dstY+yy, src.At(srcX+xx, srcY+yy))
		}
	}
}

// drawBlackTile just fills an 8x8 region with black
func drawBlackTile(dst *image.RGBA, tileX, tileY int) {
	const tileSize = 8
	dstX := tileX * tileSize
	dstY := tileY * tileSize

	for yy := 0; yy < tileSize; yy++ {
		for xx := 0; xx < tileSize; xx++ {
			dst.Set(dstX+xx, dstY+yy, pico8Palette[0])
		}
	}
}

// saveSprites writes individual sprite images plus sub-image sections
func saveSprites(spriteSheet *image.RGBA, useSection3, useSection4 bool) {
	const tileSize = 8
	const spritesPerRow = 16

	// Decide how many sprites to export
	maxSprites := 256
	if useSection3 || useSection4 {
		maxSprites = 128
	}

	// Decide how many sub-sections
	numSections := 4
	if useSection3 || useSection4 {
		numSections = 2
	}

	if err := os.MkdirAll("sprites", 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create sprites/ dir: %v\n", err)
		return
	}

	// 1) Save individual sprites
	for spriteNum := 0; spriteNum < maxSprites; spriteNum++ {
		spriteY := (spriteNum / spritesPerRow) * tileSize
		spriteX := (spriteNum % spritesPerRow) * tileSize

		sprImg := image.NewRGBA(image.Rect(0, 0, tileSize, tileSize))

		for yy := 0; yy < tileSize; yy++ {
			for xx := 0; xx < tileSize; xx++ {
				sprImg.Set(xx, yy, spriteSheet.At(spriteX+xx, spriteY+yy))
			}
		}

		spritePath := fmt.Sprintf("sprites/sprite_%03d.png", spriteNum)
		if err := saveAsPng(sprImg, spritePath); err != nil {
			fmt.Printf("Error saving %s: %v\n", spritePath, err)
		}
	}

	// 2) Save the sub-image sections (each 128x32)
	//    (section_0.png, section_1.png, ... up to numSections-1)
	const subImageHeight = 4 * tileSize // 32 px
	const subImageWidth = 16 * tileSize // 128 px

	for section := 0; section < numSections; section++ {
		subImg := image.NewRGBA(image.Rect(0, 0, subImageWidth, subImageHeight))
		startY := section * subImageHeight

		// Copy from spriteSheet
		for yy := 0; yy < subImageHeight; yy++ {
			for xx := 0; xx < subImageWidth; xx++ {
				subImg.Set(xx, yy, spriteSheet.At(xx, startY+yy))
			}
		}

		subImagePath := fmt.Sprintf("sprites/section_%d.png", section)
		if err := saveAsPng(subImg, subImagePath); err != nil {
			fmt.Printf("Error saving %s: %v\n", subImagePath, err)
		}
	}

	fmt.Printf("Saved %d sprites and %d sections into 'sprites' folder.\n", maxSprites, numSections)
}

// saveAsPng encodes the RGBA image to a PNG file
func saveAsPng(img *image.RGBA, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return png.Encode(f, img)
}

// parseHexChar interprets a single hex digit (0..F)
func parseHexChar(c rune) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// combineSectionsIntoSpriteSheet stacks section_*.png top-to-bottom
func combineSectionsIntoSpriteSheet(numSections int) error {
	// Each section_*.png is 128x32 in this code
	const subImageWidth = 128
	const subImageHeight = 32

	totalWidth := subImageWidth
	totalHeight := subImageHeight * numSections

	finalImg := image.NewRGBA(image.Rect(0, 0, totalWidth, totalHeight))

	for i := 0; i < numSections; i++ {
		sectionPath := fmt.Sprintf("sprites/section_%d.png", i)
		f, err := os.Open(sectionPath)
		if err != nil {
			return fmt.Errorf("failed to open %s: %v", sectionPath, err)
		}
		sectionImg, err := png.Decode(f)
		f.Close()
		if err != nil {
			return fmt.Errorf("failed to decode %s: %v", sectionPath, err)
		}

		// Copy the section into finalImg at the correct vertical offset
		destRect := image.Rect(0, i*subImageHeight, subImageWidth, (i+1)*subImageHeight)
		draw.Draw(finalImg, destRect, sectionImg, image.Point{0, 0}, draw.Src)
	}

	out, err := os.Create("spritesheet.png")
	if err != nil {
		return fmt.Errorf("failed to create spritesheet.png: %v", err)
	}
	defer out.Close()

	if err := png.Encode(out, finalImg); err != nil {
		return fmt.Errorf("failed to encode spritesheet.png: %v", err)
	}

	fmt.Printf("Created spritesheet.png with %d sections.\n", numSections)
	return nil
}
