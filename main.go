package main //nolint:revive

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
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

// SpriteSheet represents the complete spritesheet data for JSON output
type SpriteSheet struct {
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Sprites     []Sprite `json:"sprites"`
	Metadata    MetaData `json:"metadata"`
}

type Sprite struct {
	ID       int         `json:"id"`
	X        int         `json:"x"`
	Y        int         `json:"y"`
	Width    int         `json:"width"`
	Height   int         `json:"height"`
	Pixels   [][]int     `json:"pixels"`
	Flags    SpriteFlags `json:"flags"`
	Used     bool        `json:"used"`
	Filename string      `json:"filename"`
}

type SpriteFlags struct {
	Bitfield   int    `json:"bitfield"`
	Individual []bool `json:"individual"`
}

type MetaData struct {
	SpriteWidth      int              `json:"spriteWidth"`
	SpriteHeight     int              `json:"spriteHeight"`
	GridWidth        int              `json:"gridWidth"`
	GridHeight       int              `json:"gridHeight"`
	AvailableSprites AvailableSprites `json:"availableSprites"`
	Palette          []PaletteColor   `json:"palette"`
}

type AvailableSprites struct {
	Total    int            `json:"total"`
	Ranges   []SpriteRange  `json:"ranges"`
	Sections SpriteSections `json:"sections"`
}

type SpriteRange struct {
	Start       int    `json:"start"`
	End         int    `json:"end"`
	Used        bool   `json:"used"`
	Description string `json:"description"`
}

type SpriteSections struct {
	Base     bool `json:"base"`
	Section3 bool `json:"section3"`
	Section4 bool `json:"section4"`
}

type PaletteColor struct {
	R uint8 `json:"r"`
	G uint8 `json:"g"`
	B uint8 `json:"b"`
	A uint8 `json:"a"`
}

// MapSheet represents the complete map data for JSON output
type MapSheet struct {
	Version     string    `json:"version"`
	Description string    `json:"description"`
	Width       int       `json:"width"`
	Height      int       `json:"height"`
	Name        string    `json:"name"`
	Cells       []MapCell `json:"cells"`
}

type MapCell struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Sprite int `json:"sprite"`
}

// TilemapJSON represents a game-dev friendly tilemap format
type TilemapJSON struct {
	Version         string `json:"version"`
	Description     string `json:"description"`
	WidthInSprites  int    `json:"widthInSprites"`  // Width in sprites (128 by default)
	HeightInSprites int    `json:"heightInSprites"` // Height in sprites (32 by default, 48 with --3, 64 with --3 and --4)
	Data            []int  `json:"data"`            // 1D array of tile IDs, row-major order
}

func main() {
	// Flags: user can specify a cart path, and optional --3 or --4
	var cartPath string
	var useSection3, useSection4 bool
	var cleanSlate bool

	flag.StringVar(&cartPath, "cart", "", "Path to the PICO-8 cartridge file (.p8)")
	flag.BoolVar(&useSection3, "3", false, "Include dual-purpose section 3 (sprites 128..191)")
	flag.BoolVar(&useSection4, "4", false, "Include dual-purpose section 4 (sprites 192..255)")
	flag.BoolVar(&cleanSlate, "clean", false, "Remove old sprites directory, map.png, spritesheet.png if they exist")
	flag.Parse()

	if cartPath == "" {
		fmt.Fprintln(os.Stderr, "Error: --cart flag is required")
		flag.Usage()
		os.Exit(1)
	}

	// Clean up old artifacts if requested
	if cleanSlate {
		if err := os.RemoveAll("sprites"); err == nil {
			fmt.Println("Removed old sprites/ folder.")
		}
		if err := os.Remove("map.png"); err == nil {
			fmt.Println("Removed old map.png.")
		}
		if err := os.Remove("spritesheet.png"); err == nil {
			fmt.Println("Removed old spritesheet.png.")
		}
		if err := os.Remove("spritesheet.json"); err == nil {
			fmt.Println("Removed old spritesheet.json.")
		}
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

	// Parse flag data
	flagData := parseFlagSection(cartPath)

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
	numSections := 2 // Base sections (0-127) are always included
	if !useSection3 && !useSection4 {
		numSections = 4 // All sections available
	} else if !useSection3 {
		numSections = 3 // Section 3 available
	} else if !useSection4 {
		numSections = 3 // Section 4 available
	}
	if err := combineSectionsIntoSpriteSheet(numSections); err != nil {
		fmt.Println("Error combining sections:", err)
	}

	// Generate and save spritesheet JSON
	jsonData, err := generateSpriteSheetJSON(gfxData, flagData, useSection3, useSection4)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating spritesheet JSON: %v\n", err)
		os.Exit(1)
	}

	if err := saveSpritesheetJSON(jsonData, "spritesheet.json"); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving spritesheet.json: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Successfully generated spritesheet.json")

	// Create individual sprite PNGs
	if err := createIndividualSpritePNGs("spritesheet.json"); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating individual sprite PNGs: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Successfully created individual sprite PNGs")

	// Generate and save map JSON
	mapSheet, err := generateMapJSON(mapData, gfxData, useSection3, useSection4)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating map JSON: %v\n", err)
		os.Exit(1)
	}

	if err := saveMapJSON(mapSheet, "map.json"); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving map.json: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Successfully generated map.json")
}

// generateMapJSON creates the JSON representation of the map
func generateMapJSON(mapData, gfxData []string, useSection3, useSection4 bool) (*MapSheet, error) {
	// Calculate map dimensions
	width := 128 // Default width
	height := 32 // Default height
	if useSection3 {
		height = 48
	}
	if useSection4 {
		height = 64
	}

	mapSheet := &MapSheet{
		Version:     "1.0",
		Description: "PICO-8 map export",
		Width:       width,
		Height:      height,
		Name:        "main",
		Cells:       make([]MapCell, 0),
	}

	// Process main map layer (base section)
	for y := 0; y < 32; y++ {
		if y < len(mapData) {
			line := mapData[y]
			for x := 0; x < width; x++ {
				if x*2+1 < len(line) {
					// Each tile is represented by 2 hex digits
					spriteY := parseHexChar(rune(line[x*2]))
					spriteX := parseHexChar(rune(line[x*2+1]))
					// Convert sprite coordinates to sprite ID
					spriteID := spriteY*16 + spriteX

					cell := MapCell{
						X:      x,
						Y:      y,
						Sprite: spriteID,
					}
					mapSheet.Cells = append(mapSheet.Cells, cell)
				}
			}
		}
	}

	// Process section 3 if enabled (maps to rows 32–47)
	if useSection3 {
		startRow := 8 * 8       // 64
		endRow := startRow + 32 // 96
		if endRow > len(gfxData) {
			endRow = len(gfxData)
		}
		if startRow < len(gfxData) {
			section3Data := gfxData[startRow:endRow]
			for y := 0; y < len(section3Data); y++ {
				line := section3Data[y]
				yIsEven := (y % 2) == 0
				// Iterate over half the width of the line (64 tiles per row)
				for x := 0; x < len(line)/2; x++ {
					// First hex digit is spriteX, second is spriteY
					spriteX := parseHexChar(rune(line[x*2]))
					spriteY := parseHexChar(rune(line[x*2+1]))
					// Convert sprite coordinates to sprite ID (0-127)
					spriteID := spriteY*16 + spriteX

					if yIsEven {
						// Left half of the map (rows 32-47)
						cell := MapCell{
							X:      x,
							Y:      32 + (y / 2),
							Sprite: spriteID,
						}
						mapSheet.Cells = append(mapSheet.Cells, cell)
					} else {
						// Right half of the map (rows 32-47)
						cell := MapCell{
							X:      64 + x,
							Y:      32 + ((y - 1) / 2),
							Sprite: spriteID,
						}
						mapSheet.Cells = append(mapSheet.Cells, cell)
					}
				}
			}
		}
	}

	// Process section 4 if enabled (maps to rows 48–63)
	if useSection4 {
		startRow := 12 * 8      // 96
		endRow := startRow + 32 // 128
		if endRow > len(gfxData) {
			endRow = len(gfxData)
		}
		if startRow < len(gfxData) {
			section4Data := gfxData[startRow:endRow]
			for y := 0; y < len(section4Data); y++ {
				line := section4Data[y]
				yIsEven := (y % 2) == 0
				// Iterate over half the width of the line (64 tiles per row)
				for x := 0; x < len(line)/2; x++ {
					// First hex digit is spriteX, second is spriteY
					spriteX := parseHexChar(rune(line[x*2]))
					spriteY := parseHexChar(rune(line[x*2+1]))
					// Convert sprite coordinates to sprite ID (0-127)
					spriteID := spriteY*16 + spriteX

					if yIsEven {
						// Left half of the map (rows 48-63)
						cell := MapCell{
							X:      x,
							Y:      48 + (y / 2),
							Sprite: spriteID,
						}
						mapSheet.Cells = append(mapSheet.Cells, cell)
					} else {
						// Right half of the map (rows 48-63)
						cell := MapCell{
							X:      64 + x,
							Y:      48 + ((y - 1) / 2),
							Sprite: spriteID,
						}
						mapSheet.Cells = append(mapSheet.Cells, cell)
					}
				}
			}
		}
	}

	return mapSheet, nil
}

// parseSection reads lines between a given marker (e.g. __gfx__) until next marker __*
func parseSection(filePath, sectionName string) []string {
	f, err := os.Open(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open cart file: %v\n", err)
		return nil
	}
	defer f.Close() //nolint:errcheck

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

	// Decide which sprites to export based on map usage
	var spriteRanges []struct{ start, end int }

	// Base sprites (0-127) are always available
	spriteRanges = append(spriteRanges, struct{ start, end int }{0, 127})

	// Section 3 (128-191) is available unless --3 is used
	if !useSection3 {
		spriteRanges = append(spriteRanges, struct{ start, end int }{128, 191})
	}

	// Section 4 (192-255) is available unless --4 is used
	if !useSection4 {
		spriteRanges = append(spriteRanges, struct{ start, end int }{192, 255})
	}

	// Calculate total sprites to export
	totalSprites := 0
	for _, r := range spriteRanges {
		totalSprites += r.end - r.start + 1
	}

	// Decide how many sections to save
	// Each section is 128x32 pixels (16x4 sprites)
	numSections := 2 // Base sections (0-127) are always included
	if !useSection3 && !useSection4 {
		numSections = 4 // All sections available
	} else if !useSection3 {
		numSections = 3 // Section 3 available
	} else if !useSection4 {
		numSections = 3 // Section 4 available
	}

	if err := os.MkdirAll("sprites", 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create sprites/ dir: %v\n", err)
		return
	}

	// 1) Save individual sprites
	for _, r := range spriteRanges {
		// Skip this range if both --3 and --4 are used and it's not the base range
		if useSection3 && useSection4 && r.start > 127 {
			continue
		}

		for spriteNum := r.start; spriteNum <= r.end; spriteNum++ {
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
	}

	// 2) Save the sub-image sections (each 128x32)
	//    (section_0.png, section_1.png, ... up to numSections-1)
	const subImageHeight = 4 * tileSize // 32 px
	const subImageWidth = 16 * tileSize // 128 px

	// Save each section sequentially
	for i := 0; i < numSections; i++ {
		subImg := image.NewRGBA(image.Rect(0, 0, subImageWidth, subImageHeight))
		startY := i * subImageHeight

		// Copy from spriteSheet
		for yy := 0; yy < subImageHeight; yy++ {
			for xx := 0; xx < subImageWidth; xx++ {
				subImg.Set(xx, yy, spriteSheet.At(xx, startY+yy))
			}
		}

		subImagePath := fmt.Sprintf("sprites/section_%d.png", i)
		if err := saveAsPng(subImg, subImagePath); err != nil {
			fmt.Printf("Error saving %s: %v\n", subImagePath, err)
		}
	}

	fmt.Printf("Saved %d sprites and %d sections into 'sprites' folder.\n", totalSprites, numSections)
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
	defer f.Close() //nolint:errcheck

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

// loadImage loads an image from a file path
func loadImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, err := png.Decode(f)
	if err != nil {
		return nil, err
	}

	return img, nil
}

// combineSectionsIntoSpriteSheet combines the individual section images into a single sprite sheet
func combineSectionsIntoSpriteSheet(numSections int) error {
	const sectionWidth = 128
	const sectionHeight = 32

	// Create a new image to hold the combined sprite sheet
	// Height is based on the number of sections we're actually using
	combined := image.NewRGBA(image.Rect(0, 0, sectionWidth, sectionHeight*4)) // Always create full height

	// Combine the sections
	for i := 0; i < numSections; i++ {
		sectionPath := fmt.Sprintf("sprites/section_%d.png", i)
		sectionImg, err := loadImage(sectionPath)
		if err != nil {
			return fmt.Errorf("failed to open %s: %v", sectionPath, err)
		}

		// Copy the section into the combined image at the correct position
		destY := i * sectionHeight
		for y := 0; y < sectionHeight; y++ {
			for x := 0; x < sectionWidth; x++ {
				combined.Set(x, destY+y, sectionImg.At(x, y))
			}
		}
	}

	// Fill remaining sections with transparency
	for i := numSections; i < 4; i++ {
		destY := i * sectionHeight
		for y := 0; y < sectionHeight; y++ {
			for x := 0; x < sectionWidth; x++ {
				combined.Set(x, destY+y, color.RGBA{0, 0, 0, 0})
			}
		}
	}

	// Save the combined sprite sheet
	if err := saveAsPng(combined, "spritesheet.png"); err != nil {
		return fmt.Errorf("failed to save spritesheet.png: %v", err)
	}

	fmt.Printf("Created spritesheet.png with %d sections.\n", numSections)
	return nil
}

// parseFlagSection reads the __gff__ section and returns the flag data for each sprite
func parseFlagSection(filePath string) []int {
	flagData := make([]int, 256) // Initialize with 0s

	section := parseSection(filePath, "__gff__")
	if len(section) == 0 {
		return flagData // Return all zeros if no flag data found
	}

	// Each line in __gff__ contains 256 hex chars (2 per sprite, 128 sprites per line)
	// We need 2 lines to cover all 256 sprites
	for lineNum, line := range section {
		if lineNum >= 2 { // We only need first 2 lines
			break
		}

		// Process each pair of hex chars
		for i := 0; i < len(line)-1 && i/2 < 128; i += 2 {
			spriteIndex := (lineNum * 128) + (i / 2)
			if spriteIndex >= 256 {
				break
			}

			// Convert two hex chars to a byte
			flagValue := parseHexChar(rune(line[i]))*16 + parseHexChar(rune(line[i+1]))
			flagData[spriteIndex] = flagValue
		}
	}

	return flagData
}

// getFlagArray converts a flag byte into array of 8 booleans
func getFlagArray(flagByte int) []bool {
	flags := make([]bool, 8)
	for i := 0; i < 8; i++ {
		flags[i] = (flagByte & (1 << i)) != 0
	}
	return flags
}

// generateSpriteSheetJSON creates the JSON representation of the spritesheet
func generateSpriteSheetJSON(gfxData []string, flagData []int, useSection3, useSection4 bool) (*SpriteSheet, error) {
	spriteSheet := &SpriteSheet{
		Version:     "1.0",
		Description: "PICO-8 spritesheet export",
		Sprites:     make([]Sprite, 0),
		Metadata: MetaData{
			SpriteWidth:  8,
			SpriteHeight: 8,
			GridWidth:    16,
			GridHeight:   16,
			AvailableSprites: AvailableSprites{
				Total: 128, // Default to base sprites only
				Ranges: []SpriteRange{
					{
						Start:       0,
						End:         127,
						Used:        true,
						Description: "Base sprites",
					},
				},
				Sections: SpriteSections{
					Base:     true,
					Section3: useSection3,
					Section4: useSection4,
				},
			},
			Palette: make([]PaletteColor, len(pico8Palette)),
		},
	}

	// Convert palette to JSON format
	for i, col := range pico8Palette {
		spriteSheet.Metadata.Palette[i] = PaletteColor{
			R: col.R,
			G: col.G,
			B: col.B,
			A: col.A,
		}
	}

	// Update available sprites based on sections
	if !useSection3 {
		spriteSheet.Metadata.AvailableSprites.Ranges = append(
			spriteSheet.Metadata.AvailableSprites.Ranges,
			SpriteRange{
				Start:       128,
				End:         191,
				Used:        true,
				Description: "Section 3 sprites",
			},
		)
		spriteSheet.Metadata.AvailableSprites.Total += 64
	}
	if !useSection4 {
		spriteSheet.Metadata.AvailableSprites.Ranges = append(
			spriteSheet.Metadata.AvailableSprites.Ranges,
			SpriteRange{
				Start:       192,
				End:         255,
				Used:        true,
				Description: "Section 4 sprites",
			},
		)
		spriteSheet.Metadata.AvailableSprites.Total += 64
	}

	// Process each sprite, but only include those in available ranges
	for spriteID := 0; spriteID < 256; spriteID++ {
		// Skip if sprite is in an unused section
		if (spriteID >= 128 && spriteID < 192 && useSection3) ||
			(spriteID >= 192 && useSection4) {
			continue
		}

		x := (spriteID % 16)
		y := (spriteID / 16)

		// Create pixel data for this sprite
		pixels := make([][]int, 8)
		for i := range pixels {
			pixels[i] = make([]int, 8)
			if y*8+i < len(gfxData) {
				line := gfxData[y*8+i]
				for j := 0; j < 8 && x*8+j < len(line); j++ {
					if x*8+j < len(line) {
						pixels[i][j] = parseHexChar(rune(line[x*8+j]))
					}
				}
			}
		}

		// Check if sprite is used (not blank)
		used := false
		for _, row := range pixels {
			for _, pixel := range row {
				if pixel != 0 {
					used = true
					break
				}
			}
			if used {
				break
			}
		}

		// Create sprite entry
		sprite := Sprite{
			ID:     spriteID,
			X:      x,
			Y:      y,
			Width:  8,
			Height: 8,
			Pixels: pixels,
			Flags: SpriteFlags{
				Bitfield:   flagData[spriteID],
				Individual: getFlagArray(flagData[spriteID]),
			},
			Used:     used,
			Filename: fmt.Sprintf("sprite_%03d.png", spriteID),
		}

		spriteSheet.Sprites = append(spriteSheet.Sprites, sprite)
	}

	return spriteSheet, nil
}

// saveSpritesheetJSON saves the spritesheet data as JSON
func saveSpritesheetJSON(spriteSheet *SpriteSheet, path string) error {
	data, err := json.MarshalIndent(spriteSheet, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshaling JSON: %w", err)
	}

	return os.WriteFile(path, data, 0644)
}

// createIndividualSpritePNGs creates PNG files for each sprite from the JSON data
func createIndividualSpritePNGs(jsonPath string) error {
	// Read the JSON file
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return fmt.Errorf("error reading JSON file: %w", err)
	}

	var spriteSheet SpriteSheet
	if err := json.Unmarshal(data, &spriteSheet); err != nil {
		return fmt.Errorf("error unmarshaling JSON: %w", err)
	}

	// Create sprites directory if it doesn't exist
	if err := os.MkdirAll("sprites", 0755); err != nil {
		return fmt.Errorf("error creating sprites directory: %w", err)
	}

	// Create an image for each sprite, but only for available ranges
	for _, sprite := range spriteSheet.Sprites {
		// Check if this sprite is in an available range
		isAvailable := false
		for _, r := range spriteSheet.Metadata.AvailableSprites.Ranges {
			if sprite.ID >= r.Start && sprite.ID <= r.End {
				isAvailable = true
				break
			}
		}

		if !isAvailable {
			continue
		}

		// Create a new 8x8 image
		img := image.NewRGBA(image.Rect(0, 0, sprite.Width, sprite.Height))

		// Fill the image with the sprite's pixels
		for y := 0; y < sprite.Height; y++ {
			for x := 0; x < sprite.Width; x++ {
				colorIndex := sprite.Pixels[y][x]
				if colorIndex >= 0 && colorIndex < len(spriteSheet.Metadata.Palette) {
					col := spriteSheet.Metadata.Palette[colorIndex]
					img.Set(x, y, color.RGBA{col.R, col.G, col.B, col.A})
				} else {
					// Default to black if color index is invalid
					img.Set(x, y, color.RGBA{0, 0, 0, 255})
				}
			}
		}

		// Save the image using the filename from the sprite data
		filename := filepath.Join("sprites", sprite.Filename)
		if err := saveAsPng(img, filename); err != nil {
			return fmt.Errorf("error saving sprite %d: %w", sprite.ID, err)
		}
	}

	return nil
}

// saveMapJSON saves the map data as JSON
func saveMapJSON(mapSheet *MapSheet, path string) error {
	data, err := json.MarshalIndent(mapSheet, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshaling map JSON: %w", err)
	}

	return os.WriteFile(path, data, 0644)
}
