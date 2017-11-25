package main // import "github.com/mojlighetsministeriet/random-backgound"

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"image"
	"math/rand"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/anthonynsimon/bild/imgio"
	"github.com/anthonynsimon/bild/transform"
	lru "github.com/hashicorp/golang-lru"
	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"
	"github.com/labstack/gommon/log"
	"github.com/mojlighetsministeriet/utils"
	"github.com/mojlighetsministeriet/utils/httprequest"
)

const wikimediaSearchURL = "https://en.wikipedia.org/w/api.php?action=query&titles=Landscape&prop=images&imlimit=2&format=json"
const wikimediaFileRootURL = "https://commons.wikimedia.org/wiki/"

type imageSize struct {
	Name   string
	Width  int
	Height int
}

func (size *imageSize) String() string {
	return strconv.Itoa(size.Width) + "x" + strconv.Itoa(size.Height)
}

func (size *imageSize) Len() int {
	return size.Width * size.Height
}

type imageSizes struct {
	Sizes []imageSize
}

func (sizes *imageSizes) String() string {
	sizeParameters := make([]string, len(sizes.Sizes))

	index := 0
	for _, size := range sizes.Sizes {
		sizeParameters[index] = size.Name
		index++
	}

	return strings.Join(sizeParameters, ", ")
}

func (sizes *imageSizes) Get(name string) (size imageSize, ok bool) {
	for _, sizeCandidate := range sizes.Sizes {
		if sizeCandidate.Name == name {
			size = sizeCandidate
			ok = true
			return
		}
	}

	ok = false

	return
}

func (sizes *imageSizes) Largest() (largest imageSize) {
	largest = imageSize{}

	for _, size := range sizes.Sizes {
		if size.Len() > largest.Len() {
			largest = size
		}
	}

	return
}

type wikimediaSearchResponse struct {
	Query wikimediaSearchResponseQuery `json:"query"`
}

type wikimediaSearchResponseQuery struct {
	Pages map[string]wikimediaSearchResponsePage `json:"pages"`
}

type wikimediaSearchResponsePage struct {
	Images []wikimediaSearchResponseImage `json:"images"`
}

type wikimediaSearchResponseImage struct {
	NS    int    `json:"ns"`
	Title string `json:"title"`
}

var imageURLs []string
var imageCache *lru.ARCCache
var jsonHTTPClient httprequest.JSONClient
var httpClient httprequest.Client

func getCroppingRectangleForAspectRatio(size imageSize, newAspectRatio float64) image.Rectangle {
	aspectRatio := float64(size.Width) / float64(size.Height)

	width := size.Width
	height := size.Height

	if aspectRatio < newAspectRatio {
		fmt.Println("original is lower")
		height = int(float64(size.Width) * newAspectRatio)
	} else {
		fmt.Println("original is higher")
		width = int(float64(size.Height) * newAspectRatio)
	}
	fmt.Println("height", height)
	fmt.Println("width", width)

	croppingRectangle := image.Rect(0, 0, width, height)

	return croppingRectangle
}

func resizeAndCropImage(imageData []byte, size imageSize) (resizedImage []byte, err error) {
	originalImage, _, err := image.Decode(bytes.NewReader(imageData))
	if err != nil {
		return
	}

	boundsSize := originalImage.Bounds().Size()
	originalSize := imageSize{Width: boundsSize.X, Height: boundsSize.Y}

	resizedAspectRatio := float64(size.Width) / float64(size.Height)
	croppedImage := transform.Crop(originalImage, getCroppingRectangleForAspectRatio(originalSize, resizedAspectRatio))
	result := transform.Resize(croppedImage, size.Width, size.Height, transform.MitchellNetravali)

	var buffer bytes.Buffer
	writer := bufio.NewWriter(&buffer)
	err = imgio.JPEGEncoder(75)(writer, result)
	if err != nil {
		return
	}

	resizedImage = buffer.Bytes()

	return
}

func getImage(url string, size imageSize, sizes imageSizes) (imageResult []byte, err error) {
	cacheKey := url + "|" + size.String()

	cachedImage, found := imageCache.Get(cacheKey)
	if found == true {
		imageResult = cachedImage.([]byte)
		return
	}

	var largestImage []byte
	largestSize := sizes.Largest()
	largestImageCacheKey := url + "|" + largestSize.Name
	cachedLargestImage, found := imageCache.Get(largestImageCacheKey)
	if found == true {
		largestImage = cachedLargestImage.([]byte)
	} else {
		originalImage, imageGetError := httpClient.Get(url)
		if imageGetError != nil {
			err = imageGetError
			return
		}

		buffer := new(bytes.Buffer)
		buffer.ReadFrom(originalImage)
		resizedImage, resizeError := resizeAndCropImage(buffer.Bytes(), largestSize)
		if resizeError != nil {
			err = resizeError
			return
		}

		largestImage = resizedImage

		imageCache.Add(largestImageCacheKey, largestImage)
	}

	imageResult, resizeError := resizeAndCropImage(largestImage, size)
	if resizeError != nil {
		err = resizeError
		return
	}

	imageCache.Add(cacheKey, imageResult)

	return
}

func getImageSizes() imageSizes {
	return imageSizes{
		Sizes: []imageSize{
			imageSize{Name: "1080p", Width: 1920, Height: 1080},
			imageSize{Name: "tablet-landscape", Width: 1024, Height: 768},
			imageSize{Name: "tablet-portrait", Width: 768, Height: 1024},
			imageSize{Name: "phone-landscape", Width: 360, Height: 640},
			imageSize{Name: "phone-portrait", Width: 640, Height: 360},
		},
	}
}

func sendImage(context echo.Context) error {
	sizes := getImageSizes()

	size, ok := sizes.Get(context.Param("size"))
	if ok == false {
		return context.String(http.StatusBadRequest, "The URL needs to end with one of: "+sizes.String())
	}

	if len(imageURLs) == 0 {
		return context.String(http.StatusServiceUnavailable, "Unable to return an image at this moment, try again in a bit")
	}

	rand.Seed(time.Now().Unix())
	imageURLIndex := rand.Int() % len(imageURLs)
	image, imageError := getImage(imageURLs[imageURLIndex], size, sizes)
	if imageError != nil {
		context.Logger().Error(imageError)
		return context.String(http.StatusServiceUnavailable, "Unable to return an image at this moment, try again in a bit")
	}

	return context.Blob(http.StatusOK, "image/jpeg", image)
}

func main() {
	service := echo.New()
	service.Use(middleware.Gzip())
	service.Logger.SetLevel(log.INFO)

	wikimediaFilePageOriginalImageURLPattern := regexp.MustCompile("fullMedia.+?href=\"([^\"]+)")

	jsonHTTPClient, err := httprequest.NewJSONClient()
	if err != nil {
		panic(err)
	}

	httpClient, err := httprequest.NewClient()
	if err != nil {
		panic(err)
	}

	imageCache, err = lru.NewARC(20)
	if err != nil {
		panic(err)
	}

	go func() {
		for {
			var newImageURLs []string

			wikimediaReponse := wikimediaSearchResponse{}
			wikimediaError := jsonHTTPClient.Get(wikimediaSearchURL, &wikimediaReponse)
			if wikimediaError != nil {
				service.Logger.Error(wikimediaError)
				continue
			}

			for _, page := range wikimediaReponse.Query.Pages {
				for _, image := range page.Images {
					url := wikimediaFileRootURL + image.Title
					filePageBody, filePageError := httpClient.Get(url)
					if filePageError != nil {
						service.Logger.Error(filePageError)
						continue
					}

					buffer := new(bytes.Buffer)
					buffer.ReadFrom(filePageBody)
					matches := wikimediaFilePageOriginalImageURLPattern.FindStringSubmatch(buffer.String())
					if matches == nil {
						service.Logger.Error(errors.New("Unable to find original image on " + url + ", has mediawiki changed their HTML structure?"))
						continue
					}

					newImageURLs = append(newImageURLs, matches[1])
				}
			}

			imageURLs = newImageURLs

			time.Sleep(60 * time.Second)
		}
	}()

	service.GET("/", func(context echo.Context) error {
		sizes := getImageSizes()
		return context.Redirect(http.StatusPermanentRedirect, sizes.Largest().Name)
	})

	service.GET("/:size", sendImage)

	service.Logger.Fatal(service.Start(":" + utils.GetEnv("PORT", "80")))
}
