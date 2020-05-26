package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"text/template"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/Medium/medium-sdk-go"
	"github.com/jessevdk/go-flags"
	"github.com/yuin/goldmark"
	"gopkg.in/yaml.v2"
)

// Options receive command line parameters
type Options struct {
	MediumAccessToken string `short:"t" long:"mediumIntegrationToken" description:"Medium Access Token" required:"true" json:"medium_access_token"`
	//	MediumUser        string `short:"u" long:"mediumUserName" description:"Medium Username" required:"false" json:"medium_user_id"`
	CanonicalURL  string `short:"c" long:"canonicalURL" description:"URL of original post" required:"false" `
	FilePath      string `short:"i" long:"markdownFile" description:"Path to Markdown file to post on Medium" required:"true"`
	PublishStatus string `short:"s" long:"publishStatus" description:"Status of the post: public, draft (default), or unlisted" required:"false" default:"draft" json:"medium_publish_status"`
	OriginalNote  string `short:"p" long:"originalNote" description:"Paragraph to append pointing to original post" required:"false"`
}

func main() {
	var opts Options
	flags.Parse(&opts)

	PublishMarkdownFile2Medium(&opts)
}

// PublishMarkdownFile2Medium Publish Markdown file to Medium
func PublishMarkdownFile2Medium(opts *Options) {
	log.Println("Processing ", opts.FilePath)

	metadata, content, err := ParseInputMarkdownFile(opts.FilePath)
	if err != nil {
		log.Fatalf("Failed to parse Markdown file: %s", err)
	}

	buf := ComposeFinalMarkdown(content, opts, metadata)

	html, err := MarkdownToHTML(buf)
	if err != nil {
		log.Fatalf("Failed to convert Markdown to HTML: %s", err)
	}

	mediumClient := medium.NewClientWithAccessToken(opts.MediumAccessToken)
	user, err := mediumClient.GetUser("")
	if err != nil {
		log.Fatalf("Failed to load Medium user: %s", err)
	}
	publishStatus := medium.PublishStatus(opts.PublishStatus)

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

// FrontMatterMetadata receives minimal Front Matter metadata
type FrontMatterMetadata struct {
	Title string    `yaml:"title"`
	Date  time.Time `yaml:"date"`
	Tags  []string  `yaml:"tags"`
}

// ParseInputMarkdownFile read the Markdown file, extracting any YAML or TOML front matter
func ParseInputMarkdownFile(filePath string) (*FrontMatterMetadata, []byte, error) {

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

	// header
	buf.WriteString("# ")
	buf.WriteString(metadata.Title)
	buf.WriteRune('\n')
	buf.WriteRune('\n')

	// body
	buf.Write(content)

	// footer
	u, err := url.Parse(opts.CanonicalURL)
	if err != nil {
		log.Fatalf("Failed to parse Canonical URL: %s", err)
	}
	context := TemplateContext{
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
func MarkdownToHTML(markdown *bytes.Buffer) (string, error) {
	var buf bytes.Buffer

	err := goldmark.Convert(markdown.Bytes(), &buf)

	return buf.String(), err
}
