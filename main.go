package main // import "github.com/mojlighetsministeriet/random-background"

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"math/rand"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/anthonynsimon/bild/blur"
	"github.com/anthonynsimon/bild/imgio"
	"github.com/anthonynsimon/bild/transform"
	lru "github.com/hashicorp/golang-lru"
	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"
	"github.com/labstack/gommon/log"
	"github.com/mojlighetsministeriet/utils"
	"github.com/mojlighetsministeriet/utils/httprequest"
)

const imageQuality = 85
const instagramTagPageURL = "https://www.instagram.com/explore/tags/landskap/"
const instagramDataRegexp = "window\\._sharedData\\s*=\\s*([^;]+)"

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

type instagramTagPageData struct {
	EntryData instagramEntryData `json:"entry_data"`
}

type instagramEntryData struct {
	TagPage []instagramTagPage `json:"TagPage"`
}

type instagramTagPage struct {
	Tag instagramTag `json:"tag"`
}

type instagramTag struct {
	TopPosts instagramTopPosts `json:"top_posts"`
}

type instagramTopPosts struct {
	Nodes []instagramNode `json:"nodes"`
}

type instagramNode struct {
	ID         string `json:"id"`
	IsVideo    bool   `json:"is_video"`
	DisplaySrc string `json:"display_src"`
}

var imageURLs []string
var imageCache *lru.ARCCache

func getCroppingRectangleForAspectRatio(size imageSize, newAspectRatio float64) image.Rectangle {
	aspectRatio := float64(size.Width) / float64(size.Height)

	startX := 0
	startY := 0
	endX := size.Width
	endY := size.Height

	if aspectRatio < newAspectRatio {
		height := int(float64(size.Width)/newAspectRatio + 0.5)
		startY = (size.Height - height) / 2
		endY = startY + height
	} else if aspectRatio > newAspectRatio {
		width := int(float64(size.Height)*newAspectRatio + 0.5)
		startX = (size.Width - width) / 2
		endX = startX + width
	}

	croppingRectangle := image.Rect(startX, startY, endX, endY)

	return croppingRectangle
}

func bytesToImage(input []byte) (output image.Image, err error) {
	output, _, err = image.Decode(bytes.NewReader(input))
	return
}

func resizeAndCropImage(imageData []byte, size imageSize) (resizedImage []byte, err error) {
	originalImage, err := bytesToImage(imageData)
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
	err = imgio.JPEGEncoder(imageQuality)(writer, result)
	if err != nil {
		return
	}

	resizedImage = buffer.Bytes()

	return
}

func getOriginalImage(url string, cache *lru.ARCCache) (imageResult []byte, err error) {
	originalImageCacheKey := url + "|original"
	cachedOriginalImage, found := imageCache.Get(originalImageCacheKey)
	if found == true {
		imageResult = cachedOriginalImage.([]byte)
		return
	}

	httpClient, clientError := httprequest.NewClient()
	if clientError != nil {
		err = clientError
		return
	}

	originalImageData, imageGetError := httpClient.Get(url)
	if imageGetError != nil {
		err = imageGetError
		return
	}

	originalImage, toImageError := bytesToImage(originalImageData)
	if toImageError != nil {
		err = toImageError
		return
	}

	originalImage = blur.Gaussian(originalImage, 30)

	buffer := new(bytes.Buffer)
	writer := bufio.NewWriter(buffer)
	err = imgio.JPEGEncoder(100)(writer, originalImage)
	if err != nil {
		return
	}

	imageCache.Add(originalImageCacheKey, buffer.Bytes())

	return
}

func getImage(url string, size imageSize, cache *lru.ARCCache) (imageResult []byte, err error) {
	cacheKey := url + "|" + size.String()

	cachedImage, found := imageCache.Get(cacheKey)
	if found == true {
		imageResult = cachedImage.([]byte)
		return
	}

	originalImage, originalImageError := getOriginalImage(url, cache)
	if originalImageError != nil {
		err = originalImageError
		return
	}

	imageResult, resizeError := resizeAndCropImage(originalImage, size)
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
			imageSize{Name: "small", Width: 640, Height: 640},
			imageSize{Name: "large", Width: 1024, Height: 1024},
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
	image, imageError := getImage(imageURLs[imageURLIndex], size, imageCache)
	if imageError != nil {
		context.Logger().Error(imageError)
		return context.String(http.StatusServiceUnavailable, "Unable to return an image at this moment, try again in a bit")
	}

	return context.Blob(http.StatusOK, "image/jpeg", image)
}

func resizeLargestWorker(jobs <-chan string, sizes imageSizes) {
	largest := sizes.Largest()

	for url := range jobs {
		getImage(url, largest, imageCache)
		time.Sleep(5 * time.Second)
	}
}

func preCacheLargestImages(imageURLs []string) {
	sizes := getImageSizes()
	jobs := make(chan string, len(imageURLs))

	go resizeLargestWorker(jobs, sizes)

	for _, url := range imageURLs {
		jobs <- url
	}

	close(jobs)
}

func main() {
	service := echo.New()
	service.Use(middleware.Gzip())
	service.Logger.SetLevel(log.INFO)

	httpClient, err := httprequest.NewClient()
	if err != nil {
		panic(err)
	}

	imageCache, err = lru.NewARC(50 * len(getImageSizes().Sizes))
	if err != nil {
		panic(err)
	}

	allowedImageExtensions := regexp.MustCompile("(?i)\\.(je?pg|png)$")
	instagramDataPattern := regexp.MustCompile(instagramDataRegexp)

	go func() {
		for {
			var newImageURLs []string

			response, fetchError := httpClient.Get(instagramTagPageURL)
			if fetchError != nil {
				service.Logger.Error(fetchError)
				continue
			}

			matches := instagramDataPattern.FindStringSubmatch(string(response))
			if matches == nil {
				service.Logger.Error(errors.New("Unable to find data for images from tag page " + instagramTagPageURL + ", has instagram changed their HTML structure?"))
				continue
			}

			instagramData := instagramTagPageData{}
			insagramDataError := json.Unmarshal([]byte(matches[1]), &instagramData)
			if insagramDataError != nil {
				service.Logger.Error(errors.New("Unable to parse data from instagram tag page " + instagramTagPageURL + ", has instagram changed their HTML structure?"))
				service.Logger.Error(insagramDataError)
				continue
			}

			for _, page := range instagramData.EntryData.TagPage {
				for _, node := range page.Tag.TopPosts.Nodes {
					if node.IsVideo == false && allowedImageExtensions.Match([]byte(node.DisplaySrc)) {
						newImageURLs = append(newImageURLs, node.DisplaySrc)
					}
				}
			}

			imageURLs = newImageURLs

			preCacheLargestImages(imageURLs)

			time.Sleep(time.Hour)
		}
	}()

	service.GET("/", func(context echo.Context) error {
		sizes := getImageSizes()
		return context.Redirect(http.StatusPermanentRedirect, sizes.Largest().Name)
	})

	service.GET("/:size", sendImage)

	service.Logger.Fatal(service.Start(":" + utils.GetEnv("PORT", "80")))
}
