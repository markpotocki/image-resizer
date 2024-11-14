package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"image"
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

var ErrUnsupportedFormat = fmt.Errorf("unsupported format")
var ErrInvalidImage = fmt.Errorf("invalid image")

func main() {
	host := flag.String("host", "localhost", "host to listen on")
	port := flag.Int("port", 8080, "port to listen on")
	flag.VisitAll(func(f *flag.Flag) {
		envKey := strings.ReplaceAll(strings.ToUpper(f.Name), "-", "_")
		if value, ok := os.LookupEnv(envKey); ok {
			f.Value.Set(value)
		}
	})
	flag.Parse()

	addr := fmt.Sprintf("%s:%d", *host, *port)
	mux := http.NewServeMux()
	mux.Handle("POST /resize", Handler(HandleResize))
	mux.Handle("POST /convert", Handler(HandleConvert))

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

type Handler func(w http.ResponseWriter, r *http.Request) http.Handler

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if next := h(w, r); next != nil {
		next.ServeHTTP(w, r)
	}
}

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

func ResizeImage(ctx context.Context, r io.Reader, height, width int) ([]byte, error) {
	buf := bytes.Buffer{}
	img, format, err := image.Decode(r)
	if err != nil {
		return buf.Bytes(), ErrInvalidImage
	}

	rect := image.Rect(0, 0, width, height)
	resized := image.NewRGBA(rect)

	draw.CatmullRom.Scale(resized, rect, img, img.Bounds(), draw.Over, nil)

	switch format {
	case "jpeg":
		err := jpeg.Encode(&buf, resized, nil)
		return buf.Bytes(), err
	case "png":
		err := png.Encode(&buf, resized)
		return buf.Bytes(), err
	default:
		return buf.Bytes(), ErrUnsupportedFormat
	}
}

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

func ConvertImage(ctx context.Context, r io.Reader, format string) ([]byte, error) {
	buf := bytes.Buffer{}
	img, _, err := image.Decode(r)
	if err != nil {
		return buf.Bytes(), ErrInvalidImage
	}

	switch format {
	case "jpeg":
		err := jpeg.Encode(&buf, img, nil)
		return buf.Bytes(), err
	case "png":
		err := png.Encode(&buf, img)
		return buf.Bytes(), err
	default:
		return buf.Bytes(), ErrUnsupportedFormat
	}
}

func Image(code int, data []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		contentType := http.DetectContentType(data)
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(code)
		w.Write(data)
	}
}

func Error(code int, err error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Error: %v\n", err)
		http.Error(w, err.Error(), code)
	}
}
