package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResizeImage(t *testing.T) {
	tests := []struct {
		name      string
		format    string
		encode    func(io.Writer, image.Image) error
		ext       string
		expectErr bool
	}{
		{
			name:   "JPEG image",
			format: "jpeg",
			encode: func(w io.Writer, img image.Image) error {
				return jpeg.Encode(w, img, nil)
			},
			ext:       "jpeg",
			expectErr: false,
		},
		{
			name:   "PNG image",
			format: "png",
			encode: func(w io.Writer, img image.Image) error {
				return png.Encode(w, img)
			},
			ext:       "png",
			expectErr: false,
		},
		{
			name:   "GIF image",
			format: "gif",
			encode: func(w io.Writer, img image.Image) error {
				return gif.Encode(w, img, nil)
			},
			ext:       "gif",
			expectErr: false,
		},
		{
			name:      "Unsupported format",
			format:    "bmp",
			encode:    nil,
			ext:       "bmp",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a sample image
			img := image.NewRGBA(image.Rect(0, 0, 100, 100))
			for x := 0; x < 100; x++ {
				for y := 0; y < 100; y++ {
					img.Set(x, y, color.RGBA{uint8(x), uint8(y), 255, 255})
				}
			}

			// Encode the image to the specified format
			var buf bytes.Buffer
			if tt.encode != nil {
				err := tt.encode(&buf, img)
				assert.NoError(t, err)
			}

			// Resize the image
			resized, err := ResizeImage(context.Background(), &buf, 50, 50)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, resized)
			}
		})
	}
}

func TestError(t *testing.T) {
	tests := []struct {
		name     string
		code     int
		err      error
		expected string
	}{
		{
			name:     "BadRequest error",
			code:     http.StatusBadRequest,
			err:      fmt.Errorf("bad request"),
			expected: "bad request\n",
		},
		{
			name:     "InternalServerError error",
			code:     http.StatusInternalServerError,
			err:      fmt.Errorf("internal server error"),
			expected: "internal server error\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			handler := Error(tt.code, tt.err)
			req := httptest.NewRequest(http.MethodGet, "/", nil)

			handler.ServeHTTP(rr, req)

			assert.Equal(t, tt.code, rr.Code)
			assert.Equal(t, tt.expected, rr.Body.String())
		})
	}
}

func TestHandleResize(t *testing.T) {
	tests := []struct {
		name           string
		queryParams    string
		imageData      []byte
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "Missing parameters",
			queryParams:    "",
			imageData:      nil,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "missing required parameters\n",
		},
		{
			name:           "Invalid height",
			queryParams:    "height=abc&width=100&format=jpeg",
			imageData:      nil,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid height: abc\n",
		},
		{
			name:           "Invalid width",
			queryParams:    "height=100&width=abc&format=jpeg",
			imageData:      nil,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid width: abc\n",
		},
		{
			name:           "Valid JPEG resize",
			queryParams:    "height=50&width=50&format=jpeg",
			imageData:      createImage(t, "jpeg"),
			expectedStatus: http.StatusOK,
			expectedError:  "",
		},
		{
			name:           "Valid PNG resize",
			queryParams:    "height=50&width=50&format=png",
			imageData:      createImage(t, "png"),
			expectedStatus: http.StatusOK,
			expectedError:  "",
		},
		{
			name:           "Valid GIF resize",
			queryParams:    "height=50&width=50&format=gif",
			imageData:      createImage(t, "gif"),
			expectedStatus: http.StatusOK,
			expectedError:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/resize?"+tt.queryParams, bytes.NewReader(tt.imageData))
			rr := httptest.NewRecorder()

			handler := Handler(HandleResize)
			handler.ServeHTTP(rr, req)

			assert.Equal(t, tt.expectedStatus, rr.Code)
			if tt.expectedError != "" {
				assert.Equal(t, tt.expectedError, rr.Body.String())
			} else {
				assert.NotEmpty(t, rr.Body.Bytes())
			}
		})
	}
}

func TestHandleConvert(t *testing.T) {
	tests := []struct {
		name           string
		queryParams    string
		imageData      []byte
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "Missing format parameter",
			queryParams:    "",
			imageData:      nil,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "missing required parameter: format\n",
		},
		{
			name:           "Unsupported format",
			queryParams:    "format=bmp",
			imageData:      createImage(t, "jpeg"),
			expectedStatus: http.StatusUnprocessableEntity,
			expectedError:  "unsupported format\n",
		},
		{
			name:           "Valid JPEG to PNG conversion",
			queryParams:    "format=png",
			imageData:      createImage(t, "jpeg"),
			expectedStatus: http.StatusOK,
			expectedError:  "",
		},
		{
			name:           "Valid PNG to JPEG conversion",
			queryParams:    "format=jpeg",
			imageData:      createImage(t, "png"),
			expectedStatus: http.StatusOK,
			expectedError:  "",
		},
		{
			name:           "Valid GIF to PNG conversion",
			queryParams:    "format=png",
			imageData:      createImage(t, "gif"),
			expectedStatus: http.StatusOK,
			expectedError:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/convert?"+tt.queryParams, bytes.NewReader(tt.imageData))
			rr := httptest.NewRecorder()

			handler := Handler(HandleConvert)
			handler.ServeHTTP(rr, req)

			assert.Equal(t, tt.expectedStatus, rr.Code)
			if tt.expectedError != "" {
				assert.Equal(t, tt.expectedError, rr.Body.String())
			} else {
				assert.NotEmpty(t, rr.Body.Bytes())
			}
		})
	}
}

func TestHandleThumbnail(t *testing.T) {
	tests := []struct {
		name           string
		queryParams    string
		imageData      []byte
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "Missing width parameter",
			queryParams:    "",
			imageData:      nil,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "missing required parameter: width\n",
		},
		{
			name:           "Invalid width parameter",
			queryParams:    "width=abc",
			imageData:      nil,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid width: abc\n",
		},
		{
			name:           "Valid JPEG thumbnail",
			queryParams:    "width=50",
			imageData:      createImage(t, "jpeg"),
			expectedStatus: http.StatusOK,
			expectedError:  "",
		},
		{
			name:           "Valid PNG thumbnail",
			queryParams:    "width=50",
			imageData:      createImage(t, "png"),
			expectedStatus: http.StatusOK,
			expectedError:  "",
		},
		{
			name:           "Valid GIF thumbnail",
			queryParams:    "width=50",
			imageData:      createImage(t, "gif"),
			expectedStatus: http.StatusOK,
			expectedError:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/thumbnail?"+tt.queryParams, bytes.NewReader(tt.imageData))
			rr := httptest.NewRecorder()

			handler := Handler(HandleThumbnail)
			handler.ServeHTTP(rr, req)

			assert.Equal(t, tt.expectedStatus, rr.Code)
			if tt.expectedError != "" {
				assert.Equal(t, tt.expectedError, rr.Body.String())
			} else {
				assert.NotEmpty(t, rr.Body.Bytes())
			}
		})
	}
}

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		args     []string
		expected Flags
	}{
		{
			name:    "Default values",
			envVars: map[string]string{},
			args:    []string{},
			expected: Flags{
				Host: "localhost",
				Port: 8080,
			},
		},
		{
			name:    "Command line arguments",
			envVars: map[string]string{},
			args:    []string{"-host", "127.0.0.1", "-port", "9090"},
			expected: Flags{
				Host: "127.0.0.1",
				Port: 9090,
			},
		},
		{
			name: "Environment variables",
			envVars: map[string]string{
				"HOST": "192.168.1.1",
				"PORT": "7070",
			},
			args: []string{},
			expected: Flags{
				Host: "192.168.1.1",
				Port: 7070,
			},
		},
		{
			name: "Command line arguments override environment variables",
			envVars: map[string]string{
				"HOST": "192.168.1.1",
				"PORT": "7070",
			},
			args: []string{"-host", "10.0.0.1", "-port", "6060"},
			expected: Flags{
				Host: "10.0.0.1",
				Port: 6060,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for key, value := range tt.envVars {
				t.Setenv(key, value)
			}

			// Set command line arguments
			flag.CommandLine = flag.NewFlagSet(tt.name, flag.ExitOnError)
			os.Args = append([]string{"cmd"}, tt.args...)

			// Parse flags
			flags := ParseFlags()

			// Assert the parsed flags
			assert.Equal(t, tt.expected, flags)
		})
	}
}

func createImage(t *testing.T, format string) []byte {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))

	switch format {
	case "jpeg":
		err := jpeg.Encode(&buf, img, nil)
		if err != nil {
			panic(err)
		}
	case "png":
		err := png.Encode(&buf, img)
		if err != nil {
			panic(err)
		}
	case "gif":
		err := gif.Encode(&buf, img, nil)
		if err != nil {
			panic(err)
		}
	default:
		return buf.Bytes()
	}

	return buf.Bytes()
}
