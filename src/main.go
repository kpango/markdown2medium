package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"mime"
	"net/url"
	"os"
	"path"
	"text/template"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/Medium/medium-sdk-go"
	"github.com/giuliov/markdown2medium/mdext"
	"github.com/jessevdk/go-flags"
	"github.com/yuin/goldmark"
	"gopkg.in/yaml.v2"
	// highlighting "github.com/yuin/goldmark-highlighting"
)

var buildVersion string

// Options receive command line parameters
type Options struct {
	MediumAccessToken string `short:"t" long:"mediumIntegrationToken" description:"Medium Access Token" required:"true" json:"medium_access_token"`
	CanonicalURL      string `short:"c" long:"canonicalURL" description:"URL of original post" required:"false" `
	FilePath          string `short:"i" short:"f" long:"markdownFile" description:"Path to Markdown file to post on Medium" required:"true"`
	PublishStatus     string `short:"s" long:"publishStatus" description:"Status of the post: public, draft (default), or unlisted" required:"false" default:"draft" json:"medium_publish_status"`
	OriginalNote      string `long:"originalNote" description:"Paragraph to append pointing to original post" required:"false"`
	DryRun            bool   `long:"dryRun" description:"Run through the Markdown data but does not upload to Medium" required:"false"`
	Debug             bool   `long:"debug" description:"Additional logging and HTML dump" required:"false"`
}

func main() {

	fmt.Printf("Markdown to Medium v%s\n", buildVersion)

	var opts Options
	if _, err := flags.Parse(&opts); err != nil {
		log.Fatalf("Failed to parse arguments: %s", err)
	}

	PublishMarkdownFile2Medium(&opts)
}

// PublishMarkdownFile2Medium Publish Markdown file to Medium
func PublishMarkdownFile2Medium(opts *Options) {
	log.Println("Processing ", opts.FilePath)

	metadata, content, err := ParseFrontMatter(opts.FilePath)
	if err != nil {
		log.Fatalf("Failed to parse Markdown file: %s", err)
	}

	buf := ComposeFinalMarkdown(content, opts, metadata)

	mediumClient := medium.NewClientWithAccessToken(opts.MediumAccessToken)

	mediumImageUploader := func(originPath string) (string, error) {
		basePath := path.Dir(opts.FilePath)
		imgPath := path.Join(basePath, originPath)
		if opts.DryRun {
			log.Printf("Should upload image file %s", imgPath)
			return originPath, nil
		} else {
			log.Printf("Uploading image file %s", imgPath)
			// upload file to Medium and get image URL back
			image, err := mediumClient.UploadImage(medium.UploadOptions{
				FilePath:    imgPath,
				ContentType: mime.TypeByExtension(path.Ext(originPath)),
			})
			if err != nil {
				return originPath, err
			}
			return image.URL, nil
		}
	}

	html, err := MarkdownToHTML(buf, mediumImageUploader)
	if err != nil {
		log.Fatalf("Failed to convert Markdown to HTML: %s", err)
	}

	if opts.Debug {
		f := path.Base(opts.FilePath) + ".html"
		ioutil.WriteFile(f, []byte(html), 0644)
	}

	user, err := mediumClient.GetUser("")
	if err != nil {
		log.Fatalf("Failed to load Medium user: %s", err)
	}
	publishStatus := medium.PublishStatus(opts.PublishStatus)

	if opts.DryRun {
		fmt.Printf("Post %s not published (dry run)\n", opts.FilePath)
	} else {
		post, err := mediumClient.CreatePost(medium.CreatePostOptions{
			UserID:        user.ID,
			Title:         metadata.Title,
			Content:       html,
			ContentFormat: medium.ContentFormatHTML,
			Tags:          metadata.Tags,
			PublishStatus: publishStatus,
			CanonicalURL:  opts.CanonicalURL,
		})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("New %s post published at %s\n", post.PublishState, post.URL)
	}
}

// FrontMatterMetadata receives minimal Front Matter metadata
type FrontMatterMetadata struct {
	Title string    `yaml:"title"`
	Date  time.Time `yaml:"date"`
	Tags  []string  `yaml:"tags"`
}

// ParseFrontMatter read the Markdown file, extracting any YAML or TOML front matter
func ParseFrontMatter(filePath string) (*FrontMatterMetadata, []byte, error) {

	var metadata FrontMatterMetadata
	var content []byte

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		log.Fatalf("File does not exist: %s", filePath)
	}

	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.Fatalf("Failed to read %s: %s", filePath, err)
	}

	// CAVEAT: assume UTF-8 input!
	data = skipUnicodeBom(data)

	var YAMLFrontMatterDelimiter = []byte("---")
	var TOMLFrontMatterDelimiter = []byte("+++")

	if bytes.HasPrefix(data, YAMLFrontMatterDelimiter) {
		parts := bytes.SplitN(data, YAMLFrontMatterDelimiter, 3)
		content = parts[2]
		err = yaml.Unmarshal(parts[1], &metadata)
	} else if bytes.HasPrefix(data, TOMLFrontMatterDelimiter) {
		parts := bytes.SplitN(data, TOMLFrontMatterDelimiter, 3)
		content = parts[2]
		_, err = toml.Decode(string(parts[1]), &metadata)
	} else {
		content = data
	}

	return &metadata, content, err
}

func skipUnicodeBom(data []byte) []byte {
	const (
		bom0 = 0xef
		bom1 = 0xbb
		bom2 = 0xbf
	)

	if len(data) >= 3 &&
		data[0] == bom0 &&
		data[1] == bom1 &&
		data[2] == bom2 {
		return data[3:]
	}
	return data
}

// TemplateContext is used by Go template
type TemplateContext struct {
	BaseURL      string
	CanonicalURL string
	Title        string
	Date         time.Time
}

// ComposeFinalMarkdown add some header and footer content to the original post
func ComposeFinalMarkdown(content []byte, opts *Options, metadata *FrontMatterMetadata) *bytes.Buffer {

	buf := new(bytes.Buffer)

	// header: Medium Story (aka post) title
	// TODO should be controlled by flag
	buf.WriteString("# ")
	buf.WriteString(metadata.Title)
	buf.WriteRune('\n')
	buf.WriteRune('\n')

	// body: source post content
	buf.Write(content)

	// footer: Originally published note
	// TODO should be controlled by opts.OriginalNote, if missing, skip
	u, err := url.Parse(opts.CanonicalURL)
	if err != nil {
		log.Fatalf("Failed to parse Canonical URL: %s", err)
	}
	context := TemplateContext{
		// fields available in template
		BaseURL:      fmt.Sprintf("%s//%s", u.Scheme, u.Host),
		CanonicalURL: opts.CanonicalURL,
		Title:        metadata.Title,
		Date:         metadata.Date,
	}
	t := template.Must(template.New("origin").Parse(opts.OriginalNote))
	buf.WriteRune('\n')
	buf.WriteRune('\n')
	err = t.Execute(buf, context)
	if err != nil {
		log.Fatalf("Executing template: %s", err)
	}

	return buf
}

// MarkdownToHTML converts Markdown to HTML using goldmark
func MarkdownToHTML(source *bytes.Buffer, imagePathHandler mdext.ImagePathHandler) (string, error) {

	markdown := goldmark.New(
		goldmark.WithExtensions(
			// TODO Medium hates this?
			// // TODO expose some options
			// highlighting.NewHighlighting(
			// 	highlighting.WithStyle("monokai"),
			// 	highlighting.WithFormatOptions(
			// 		html.WithLineNumbers(true),
			// 	),
			// ),
			mdext.NewImageExt(imagePathHandler),
		),
	)
	var buf bytes.Buffer
	err := markdown.Convert(source.Bytes(), &buf)

	return buf.String(), err
}
