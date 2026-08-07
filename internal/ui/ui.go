package ui

import (
	"fmt"

	"github.com/fatih/color"
)

var (
	cyan   = color.New(color.FgHiCyan)
	green  = color.New(color.FgHiGreen)
	yellow = color.New(color.FgHiYellow)
	red    = color.New(color.FgHiRed)
	bold   = color.New(color.Bold)
	dimmed = color.New(color.FgHiBlack)
	white  = color.New(color.FgHiWhite, color.Bold)

	// Brand colors — matched to the IdliStack logo mark (24-bit RGB).
	// fatih/color falls back to the closest ANSI color automatically
	// on terminals without truecolor support.
	magenta      = color.RGB(225, 82, 193)  // outer swoosh of the "a"
	magentaLight = color.RGB(232, 132, 233) // inner loop of the "a"
)

// PrintBanner displays the IdliStack logo, reproduced as a Braille dot-matrix
// rendering of the actual brand mark (see logo-black.png) rather than block
// lettering, so the terminal banner matches the logo shape 1:1 — including
// the two-tone "a".
func PrintBanner() {
	fmt.Println()

	// Line 1
	white.Print("  ⠀⢀⣀⣀⠀⠀⢀⣀⣀⣀⣀⣀⣀⡀⠀⠀⠀⠀⠀⣀⣀⣀⠀⠀⠀⠀⠀⠀⣀⣀⣀⠀⠀⠀⢀⣀⣠⣤⣀⡀⠀⠀⠀⣀⣀⣀⣀⣀⣀⣀⣀⣀⠀⠀⠀⠀⠀⠀")
	magentaLight.Print("⣀⣠⣤⣀")
	white.Println("⠀⠀⠀⠀⠀⠀⠀⠀⣀⣀⣤⣄⣀⡀⠀⠀⠀⢀⣀⣀⡀⠀⠀⠀⣀⣀⡀⠀")

	// Line 2
	white.Print("  ⠀⣿⣿⣿⡇⠀⣾⣿⣿⣿⣿⣿⣿⣿⣷⣄⠀⠀⢰⣿⣿⣿⡇⠀⠀⠀⠀⢸⣿⣿⣿⠀⠀⣴⣿⣿⣿⣿⣿⣿⣷⡄⢸⣿⣿⣿⣿⣿⣿⣿⣿⣿⡆⠀⠀⠀⠀")
	magentaLight.Print("⣾⣿⣿⣿⣿⣷⡀")
	white.Println("⠀⠀⠀⢀⣴⣾⣿⣿⣿⣿⣿⣿⣷⡀⠀⢸⣿⣿⣿⠀⢀⣾⣿⣿⡿⠀")

	// Line 3
	white.Print("  ⠀⠻⠿⠟⠃⠀⣿⣿⣿⣿⠿⠿⠿⣿⣿⣿⣧⠀⢸⣿⣿⣿⡇⠀⠀⠀⠀⠘⠻⠿⠟⠀⢸⣿⣿⣿⡟⠛⠿⡿⠟⠁⠈⠻⠿⢿⣿⣿⣿⡿⠿⠛⠀⠀⠀⠀")
	magentaLight.Print("⠸⠿⠋⠙⣿⣿⣿⡇")
	white.Println("⠀⠀⠀⢾⣿⣿⣿⠟⠛⠛⠿⠿⠋⠀⠀⢸⣿⣿⣿⢀⣾⣿⣿⡟⠁⠀")

	// Line 4
	white.Print("  ⠀⣿⣿⣿⡆⠀⣿⣿⣿⣿⠀⠀⠀⠸⣿⣿⣿⡇⢸⣿⣿⣿⡇⠀⠀⠀⠀⢰⣿⣿⣿⠀⠈⢿⣿⣿⣿⣶⣦⣤⡀⠀⠀⠀⠀⢸⣿⣿⣿⡇⠀⠀")
	magenta.Print("⢀⣴⣶⣾⣷⣶⣶⣄")
	magentaLight.Print("⣿⣿⣿⣧")
	white.Println("⠀⠀⢰⣶⣦⣍⠁⠀⠀⠀⠀⠀⠀⠀⠀⢸⣿⣿⣿⣿⣿⣿⡟⠀⠀⠀")

	// Line 5
	white.Print("  ⠀⣿⣿⣿⡇⠀⣿⣿⣿⣿⠀⠀⠀⢰⣿⣿⣿⡇⢸⣿⣿⣿⡇⠀⣀⡀⠀⢸⣿⣿⣿⠀⠀⠀⠙⠿⢿⣿⣿⣿⣿⣧⠀⠀⠀⢸⣿⣿⣿⡇⠀")
	magenta.Print("⢠⣿⣿⡏⠁⠉⠙⠻⡿")
	magentaLight.Print("⣿⣿⣿⣿")
	white.Println("⠀⠀⢸⣿⣿⣿⡄⠀⠀⠀⠀⠀⠀⠀⠀⢸⣿⣿⣿⣿⣿⣿⣦⠀⠀⠀")

	// Line 6
	white.Print("  ⠀⣿⣿⣿⡇⠀⣿⣿⣿⣿⣤⣤⣴⣿⣿⣿⡿⠁⢸⣿⣿⣿⣧⣾⣿⣿⡇⢸⣿⣿⣿⠀⢀⣴⣾⣦⣄⣀⣹⣿⣿⣿⠀⠀⠀⢸⣿⣿⣿⡇⠀")
	magenta.Print("⠸⣿⣿⣷⣤⣤⣤⣤⣤")
	magentaLight.Print("⣿⣿⣿⣿")
	magenta.Print("⣿⣦")
	white.Println("⠈⢿⣿⣿⣿⣤⣀⣠⣴⣿⣦⣄⠀⢸⣿⣿⣿⠘⢿⣿⣿⣧⡀⠀")

	// Line 7
	white.Print("  ⠀⣿⣿⣿⡇⠀⣿⣿⣿⣿⣿⣿⣿⣿⣿⠟⠁⠀⢸⣿⣿⣿⣿⣿⣿⣿⡇⢸⣿⣿⣿⠀⠘⢿⣿⣿⣿⣿⣿⣿⣿⠟⠀⠀⠀⢸⣿⣿⣿⡇⠀⠀")
	magenta.Print("⠹⢿⣿⣿⣿⣿⣿⣿")
	magentaLight.Print("⣿⣿⣿⡿")
	magenta.Print("⣿⣿⡇")
	white.Println("⠈⠻⣿⣿⣿⣿⣿⣿⣿⣿⠟⠀⢸⣿⣿⣿⠀⠈⢿⣿⣿⣷⠀")

	// Line 8
	white.Print("  ⠀⠙⠛⠛⠁⠀⠈⠛⠛⠛⠛⠛⠋⠉⠀⠀⠀⠀⠀⠙⠛⠛⠛⠛⠛⠛⠁⠈⠙⠛⠋⠀⠀⠀⠉⠛⠛⠛⠛⠋⠁⠀⠀⠀⠀⠀⠙⠛⠋⠀⠀⠀⠀⠀")
	magenta.Print("⠉⠉⠛⠛⠛⠛⠛")
	magentaLight.Print("⠛⠛⠁")
	magenta.Print("⠈⠋⠁")
	white.Println("⠀⠀⠈⠉⠛⠛⠛⠛⠉⠀⠀⠀⠈⠛⠛⠁⠀⠀⠈⠙⠛⠋⠀")

	fmt.Println()
	dimmed.Println("                                                                                 by T4GC")
	dimmed.Println("  Deploy with zero configuration")
	fmt.Println()
}

// Step displays a numbered step in the pipeline
func Step(current, total int, msg string) {
	fmt.Println()
	stepColor := color.New(color.FgHiCyan, color.Bold)
	stepColor.Printf("  [%d/%d] %s\n", current, total, msg)
}

// Detail displays an indented detail line
func Detail(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Printf("        %s\n", msg)
}

// Success displays a success message
func Success(msg string) {
	green.Printf("  ✓ %s\n", msg)
}

// Warn displays a warning message
func Warn(msg string) {
	yellow.Printf("  ⚠ %s\n", msg)
}

// Error displays an error message
func Error(msg string) {
	red.Printf("  ✗ %s\n", msg)
}

// Info displays an info message
func Info(msg string) {
	dimmed.Printf("  ℹ %s\n", msg)
}

// PrintDivider prints a horizontal line
func PrintDivider() {
	dimmed.Println("  ─────────────────────────────────────────")
}