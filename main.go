package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"golang.org/x/image/draw"
)

var (
	// ErrUnsupportedFormat is returned when an unsupported image format is encountered
	ErrUnsupportedFormat = fmt.Errorf("unsupported format")
	// ErrInvalidImage is returned when an invalid image is encountered
	ErrInvalidImage = fmt.Errorf("invalid image")
)

const (
	// Supported image formats
	formatJPEG = "jpeg"
	formatPNG  = "png"
	formatGIF  = "gif"
)

func main() {
	flags := ParseFlags()

	addr := fmt.Sprintf("%s:%d", flags.Host, flags.Port)
	mux := http.NewServeMux()
	mux.Handle("POST /resize", Handler(HandleResize))
	mux.Handle("POST /convert", Handler(HandleConvert))
	mux.Handle("POST /thumbnail", Handler(HandleThumbnail))

	shutdownCtx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		log.Printf("Listening on %s\n", addr)
		log.Println(srv.ListenAndServe())
	}()

	<-shutdownCtx.Done()
	log.Println("Shutting down...")
	srv.Shutdown(context.Background())
}

// Flags is a struct that holds the command-line flags for the application.
type Flags struct {
	// Host is the host to listen on
	Host string
	// Port is the port to listen on
	Port int
}

// ParseFlags parses the command-line flags and returns a Flags struct.
func ParseFlags() Flags {
	host := flag.String("host", "localhost", "host to listen on")
	port := flag.Int("port", 8080, "port to listen on")
	flag.VisitAll(func(f *flag.Flag) {
		envKey := strings.ReplaceAll(strings.ToUpper(f.Name), "-", "_")
		if value, ok := os.LookupEnv(envKey); ok {
			f.Value.Set(value)
		}
	})
	flag.Parse()
	return Flags{*host, *port}
}

// Handler is a type that wraps an http.Handler with a custom handler function.
type Handler func(w http.ResponseWriter, r *http.Request) http.Handler

// ServeHTTP handles HTTP requests and responses. It calls the handler function
// and, if the handler returns a non-nil next handler, it delegates the request
// to the next handler's ServeHTTP method.
func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if next := h(w, r); next != nil {
		next.ServeHTTP(w, r)
	}
}

// HandleResize handles the image resizing request. It reads the height and width
// parameters from the query string, validates them, and attempts to resize the
// image accordingly. If the parameters are missing or invalid, it returns an
// appropriate error response. If the image format is unsupported or another
// error occurs during resizing, it returns the corresponding error response.
// On success, it returns the resized image.
//
// Query Parameters:
// - height: The desired height of the resized image (required).
// - width: The desired width of the resized image (required).
//
// Responses:
// - 400 Bad Request: If the height or width parameters are missing or invalid.
// - 422 Unprocessable Entity: If the image format is unsupported.
// - 500 Internal Server Error: If an error occurs during resizing.
// - 200 OK: If the image is successfully resized.
func HandleResize(w http.ResponseWriter, r *http.Request) http.Handler {
	// Parse query parameters
	params := r.URL.Query()
	heightParam := params.Get("height")
	widthParam := params.Get("width")

	// Validate query parameters
	if heightParam == "" || widthParam == "" {
		return Error(http.StatusBadRequest, fmt.Errorf("missing required parameters"))
	}

	// Parse height and width
	height, err := strconv.Atoi(heightParam)
	if err != nil {
		return Error(http.StatusBadRequest, fmt.Errorf("invalid height: %s", heightParam))
	}

	width, err := strconv.Atoi(widthParam)
	if err != nil {
		return Error(http.StatusBadRequest, fmt.Errorf("invalid width: %s", widthParam))
	}

	// Resize image
	resized, err := ResizeImage(r.Context(), r.Body, height, width)
	if err == ErrUnsupportedFormat {
		return Error(http.StatusUnprocessableEntity, err)
	}
	if err != nil {
		return Error(http.StatusInternalServerError, err)
	}

	// Return resized image
	return Image(http.StatusOK, resized)
}

// ResizeImage resizes an image to the specified height and width.
// It takes a context for cancellation, an io.Reader to read the image,
// and the desired height and width of the resized image.
// It returns the resized image as a byte slice and an error if any occurred.
//
// Parameters:
//
//	ctx - context for cancellation
//	r - io.Reader to read the image
//	height - desired height of the resized image
//	width - desired width of the resized image
//
// Returns:
//
//	[]byte - the resized image as a byte slice
//	error - an error if any occurred during the resizing process
func ResizeImage(ctx context.Context, r io.Reader, height, width int) ([]byte, error) {
	img, format, err := image.Decode(r)
	if err != nil {
		return nil, ErrInvalidImage
	}

	rect := image.Rect(0, 0, width, height)
	resized := image.NewRGBA(rect)

	draw.CatmullRom.Scale(resized, rect, img, img.Bounds(), draw.Over, nil)

	return EncodeImage(ctx, resized, format)
}

// HandleConvert handles the image conversion request. It parses the query parameters,
// validates them, converts the image to the specified format, and returns the converted image.
//
// Query Parameters:
// - format: The desired image format (e.g., "jpeg", "png").
//
// Responses:
// - 400 Bad Request: If the required "format" parameter is missing.
// - 422 Unprocessable Entity: If the specified format is unsupported.
// - 500 Internal Server Error: If an error occurs during image conversion.
// - 200 OK: If the image is successfully converted and returned.
func HandleConvert(w http.ResponseWriter, r *http.Request) http.Handler {
	// Parse query parameters
	params := r.URL.Query()
	format := params.Get("format")

	// Validate query parameters
	if format == "" {
		return Error(http.StatusBadRequest, fmt.Errorf("missing required parameter: format"))
	}

	// Convert image
	converted, err := ConvertImage(r.Context(), r.Body, format)
	if err == ErrUnsupportedFormat {
		return Error(http.StatusUnprocessableEntity, err)
	}
	if err != nil {
		return Error(http.StatusInternalServerError, err)
	}

	// Return converted image
	return Image(http.StatusOK, converted)
}

// ConvertImage reads an image from the provided io.Reader, decodes it, and then encodes it into the specified format.
// The function takes a context for managing timeouts and cancellations, an io.Reader from which the image is read,
// and a string specifying the desired output format (e.g., "jpeg", "png").
// It returns the encoded image as a byte slice or an error if the decoding or encoding fails.
func ConvertImage(ctx context.Context, r io.Reader, format string) ([]byte, error) {
	img, _, err := image.Decode(r)
	if err != nil {
		return nil, ErrInvalidImage
	}

	return EncodeImage(ctx, img, format)
}

// HandleThumbnail handles the generation of a thumbnail image based on the provided width query parameter.
// It expects the width parameter to be present in the query string and to be a valid integer.
// If the width parameter is missing or invalid, it returns an appropriate error response.
// It generates the thumbnail image using the provided image data in the request body and the specified width.
// If the image format is unsupported, it returns an unprocessable entity error.
// If any other error occurs during thumbnail generation, it returns an internal server error.
// On success, it returns the generated thumbnail image with an HTTP status OK.
func HandleThumbnail(w http.ResponseWriter, r *http.Request) http.Handler {
	// Parse query parameters
	params := r.URL.Query()
	widthParam := params.Get("width")

	// Validate query parameters
	if widthParam == "" {
		return Error(http.StatusBadRequest, fmt.Errorf("missing required parameter: width"))
	}

	// Parse width
	width, err := strconv.Atoi(widthParam)
	if err != nil {
		return Error(http.StatusBadRequest, fmt.Errorf("invalid width: %s", widthParam))
	}

	// Generate thumbnail
	thumbnail, err := ThumbnailImage(r.Context(), r.Body, width)
	if err == ErrUnsupportedFormat {
		return Error(http.StatusUnprocessableEntity, err)
	}
	if err != nil {
		return Error(http.StatusInternalServerError, err)
	}

	// Return thumbnail
	return Image(http.StatusOK, thumbnail)
}

// ThumbnailImage resizes an image to the specified width while maintaining the aspect ratio.
// It reads the image from the provided io.Reader, decodes it, and then scales it to the new dimensions.
// The resized image is then encoded back to the original format and returned as a byte slice.
//
// Parameters:
//   - ctx: The context for managing the lifecycle of the request.
//   - r: An io.Reader from which the image is read.
//   - width: The desired width of the resized image.
//
// Returns:
//   - A byte slice containing the resized image.
//   - An error if the image decoding or encoding fails.
//
// Possible errors:
//   - ErrInvalidImage: If the image cannot be decoded.
func ThumbnailImage(ctx context.Context, r io.Reader, width int) ([]byte, error) {
	img, format, err := image.Decode(r)
	if err != nil {
		return nil, ErrInvalidImage
	}

	rect := img.Bounds()
	height := rect.Dy() * width / rect.Dx()
	rect = image.Rect(0, 0, width, height)
	resized := image.NewRGBA(rect)

	draw.CatmullRom.Scale(resized, rect, img, img.Bounds(), draw.Over, nil)

	return EncodeImage(ctx, resized, format)
}

// EncodeImage encodes an image.Image into the specified format and returns the encoded bytes.
// Supported formats are "jpeg" and "png". If an unsupported format is provided, it returns an error.
//
// Parameters:
//
//	ctx - The context for the encoding operation.
//	img - The image to be encoded.
//	format - The format to encode the image in ("jpeg" or "png").
//
// Returns:
//
//	A byte slice containing the encoded image data, and an error if the encoding fails or the format is unsupported.
func EncodeImage(ctx context.Context, img image.Image, format string) ([]byte, error) {
	buf := bytes.Buffer{}
	switch format {
	case formatJPEG:
		err := jpeg.Encode(&buf, img, nil)
		return buf.Bytes(), err
	case formatPNG:
		err := png.Encode(&buf, img)
		return buf.Bytes(), err
	case formatGIF:
		err := gif.Encode(&buf, img, nil)
		return buf.Bytes(), err
	default:
		return buf.Bytes(), ErrUnsupportedFormat
	}
}

// Image returns an http.HandlerFunc that serves the provided data with the specified HTTP status code.
// The Content-Type header is set based on the detected content type of the data, and the Content-Length
// header is set to the length of the data.
//
// Parameters:
//   - code: The HTTP status code to be used in the response.
//   - data: The byte slice containing the data to be served.
//
// Returns:
//
//	An http.HandlerFunc that writes the data to the response with the specified headers and status code.
func Image(code int, data []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		contentType := http.DetectContentType(data)
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(code)
		w.Write(data)
	}
}

// Error returns an http.HandlerFunc that logs the provided error and sends an HTTP error response with the specified status code.
// Parameters:
//   - code: The HTTP status code to be sent in the response.
//   - err: The error to be logged and sent in the response body.
//
// Returns:
//
//	An http.HandlerFunc that handles the error response.
func Error(code int, err error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Error: %v\n", err)
		http.Error(w, err.Error(), code)
	}
}
