package thoughts

// Type represents the model reasoning before it answers: what it said to
// itself, and the signature that says the block came from the model. Both are
// carried as they arrived and neither is read here.
//
// A redacted thought is the same shape with its reasoning withheld: what is
// left is an opaque blob, which is still the model's and still has to go back.
type Type struct {
	text      string
	signature string
	data      string
	redacted  bool
}

// Text is the reasoning as words, which is often nothing at all: an API asked
// to keep its reasoning to itself returns a thought with a signature and an
// empty text, and that is a whole thought rather than half of one.
func (thought Type) Text() string {
	return thought.text
}

// Signature is the model's proof that it wrote the thought. It is opaque and
// is passed back untouched.
func (thought Type) Signature() string {
	return thought.signature
}

// Data is the blob a redacted thought carries, and the bool is what tells a
// redacted thought from an ordinary one. They come as a pair because the blob
// means nothing without knowing it is one.
func (thought Type) Data() (string, bool) {
	return thought.data, thought.redacted
}

// New builds an ordinary thought from what the model said and the signature it
// said it under.
func New(text, signature string) Type {
	return Type{text: text, signature: signature}
}

// Redacted builds the thought whose reasoning was withheld. It carries only
// the blob, which is the whole of what there is to carry.
func Redacted(data string) Type {
	return Type{data: data, redacted: true}
}
