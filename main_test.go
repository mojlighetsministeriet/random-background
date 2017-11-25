package main

import (
	"image"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetCroppingRectangleForAspectRatioWhenOriginalRatioIsLess(test *testing.T) {
	expectedOutput := image.Rect(0, 357, 3000, 1688+357)

	originalSize := imageSize{Width: 3000, Height: 2402}
	newSize := imageSize{Width: 1920, Height: 1080}
	newAspectRatio := float64(newSize.Width) / float64(newSize.Height)
	output := getCroppingRectangleForAspectRatio(originalSize, newAspectRatio)

	assert.Equal(test, expectedOutput, output)
}

func TestGetCroppingRectangleForAspectRatioWhenOriginalRatioIsLess2(test *testing.T) {
	expectedOutput := image.Rect(0, 824, 2402, 1351+824)

	originalSize := imageSize{Width: 2402, Height: 3000}
	newSize := imageSize{Width: 1920, Height: 1080}
	newAspectRatio := float64(newSize.Width) / float64(newSize.Height)
	output := getCroppingRectangleForAspectRatio(originalSize, newAspectRatio)

	assert.Equal(test, expectedOutput, output)
}

func TestGetCroppingRectangleForAspectRatioWhenOriginalRatioIsGreater(test *testing.T) {
	expectedOutput := image.Rect(611, 0, 1778+611, 1000)

	originalSize := imageSize{Width: 3000, Height: 1000}
	newSize := imageSize{Width: 1920, Height: 1080}
	newAspectRatio := float64(newSize.Width) / float64(newSize.Height)
	output := getCroppingRectangleForAspectRatio(originalSize, newAspectRatio)

	assert.Equal(test, expectedOutput, output)
}

func TestGetCroppingRectangleForAspectRatioWhenOriginalRatioIsGreater2(test *testing.T) {
	expectedOutput := image.Rect(357, 0, 1688+357, 3000)

	originalSize := imageSize{Width: 2402, Height: 3000}
	newSize := imageSize{Width: 360, Height: 640}
	newAspectRatio := float64(newSize.Width) / float64(newSize.Height)
	output := getCroppingRectangleForAspectRatio(originalSize, newAspectRatio)

	assert.Equal(test, expectedOutput, output)
}
