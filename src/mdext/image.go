// source https://github.com/jschaf/b2/
package mdext

import (
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// ImagePathHandler is
type ImagePathHandler func(string) (string, error)

// ImagePathHandlerConfig is
type ImagePathHandlerConfig struct {
	imagePathHandler ImagePathHandler
}

// imageASTTransformer extracts images we should copy over to the public dir
// when publishing posts.
type imageASTTransformer struct {
	ImagePathHandlerConfig
}

func (f imageASTTransformer) Transform(doc *ast.Document, _ text.Reader, pc parser.Context) {
	err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkSkipChildren, nil
		}
		if n.Kind() != ast.KindImage {
			return ast.WalkContinue, nil
		}
		img := n.(*ast.Image)
		origDest := string(img.Destination)
		if strings.HasPrefix(origDest, "http://") || strings.HasPrefix(origDest, "https://") {
			// already an URL
			return ast.WalkContinue, nil
		}

		imagePathHandler := f.imagePathHandler
		newDest, err := imagePathHandler(origDest)
		img.Destination = []byte(newDest)

		return ast.WalkSkipChildren, err
	})

	if err != nil {
		panic(err)
	}
}

// imageRenderer writes images into HTML, replacing the default image renderer.
type imageRenderer struct{}

func (ir imageRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindImage, ir.renderImage)
}

func (ir imageRenderer) renderImage(w util.BufWriter, source []byte, node ast.Node, entering bool) (status ast.WalkStatus, err error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.Image)
	// TODO
	// <figure>
	// 	<img src="bey.png">
	// 	<figcaption>I am an optional caption</figcaption>
	// </figure>
	tag := fmt.Sprintf(
		"<img src=%q alt=%q title=%q",
		n.Destination, n.Text(source), n.Title)
	_, _ = w.WriteString(tag)
	if n.Attributes() != nil {
		html.RenderAttributes(w, n, html.ImageAttributeFilter)
	}
	_, _ = w.WriteString(">")
	return ast.WalkSkipChildren, nil
}

// ImageExt extends markdown with the transformer and renderer.
type ImageExt struct {
	ImagePathHandlerConfig
}

// NewImageExt is
func NewImageExt(imagePathHandler ImagePathHandler) *ImageExt {
	return &ImageExt{
		ImagePathHandlerConfig: ImagePathHandlerConfig{
			imagePathHandler: imagePathHandler,
		},
	}
}

func newImageASTTransformer(cfg ImagePathHandlerConfig) *imageASTTransformer {
	p := &imageASTTransformer{
		ImagePathHandlerConfig: ImagePathHandlerConfig{
			imagePathHandler: cfg.imagePathHandler,
		},
	}
	return p
}

// Extend is
func (i *ImageExt) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		parser.WithASTTransformers(
			util.Prioritized(newImageASTTransformer(i.ImagePathHandlerConfig), 999)))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(imageRenderer{}, 500),
	))
}
