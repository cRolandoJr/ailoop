package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cRolandoJr/ailoop/internal/llm"
	"github.com/cRolandoJr/ailoop/internal/patch"
)

// maxImageBytes bounds a single attachment.
const maxImageBytes = 5 << 20 // 5 MiB

// imageTypes maps the extensions every vision-capable provider accepts.
var imageTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
}

func readImage(workspace, arg string) (llm.Image, error) {
	rel, err := patch.SafeRelPath(workspace, arg)
	if err != nil {
		return llm.Image{}, err
	}

	mediaType, ok := imageTypes[strings.ToLower(filepath.Ext(rel))]
	if !ok {
		return llm.Image{}, fmt.Errorf("%q is not a supported image type (png, jpeg, gif, webp)", rel)
	}

	info, err := os.Stat(filepath.Join(workspace, rel))
	if err != nil {
		return llm.Image{}, err
	}
	if info.Size() > maxImageBytes {
		return llm.Image{}, fmt.Errorf("image is %d bytes, over the %d limit", info.Size(), maxImageBytes)
	}

	data, err := os.ReadFile(filepath.Join(workspace, rel))
	if err != nil {
		return llm.Image{}, err
	}
	return llm.Image{MediaType: mediaType, Data: data}, nil
}
