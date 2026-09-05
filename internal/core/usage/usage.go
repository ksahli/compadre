package usage

// Type is what one turn cost: the tokens the request was read from and the
// tokens the reply was written in. The fields are unexported, so the only way
// to a counted value is [New] and the only other value is the zero Type, which
// is no count at all.
//
// It is comparable, so two counts are matched with ==.
type Type struct {
	input   int64
	output  int64
	counted bool
}

// Input is how many tokens were read to produce the turn. On a provider that
// sends the whole thread each time, this is the cost of everything said so far
// and not of this turn alone, which is why counts are not added up across a
// conversation.
func (count Type) Input() int64 {
	return count.input
}

// Output is how many tokens the turn was written in.
func (count Type) Output() int64 {
	return count.output
}

// Counted says whether anybody counted this turn. It is false for the zero
// Type and true for everything [New] builds, including a count of zero: a turn
// measured at nothing and a turn nobody measured are different facts, and only
// the first is worth showing a reader.
func (count Type) Counted() bool {
	return count.counted
}

// New builds a count from what was read and what was written. A negative
// number is taken as zero rather than refused: this returns a value and has
// nowhere to report an error to, and no meter counts backwards, so a negative
// is a provider mismapping its own response rather than something a caller
// could act on. The count is still marked as taken, because it was.
func New(input, output int64) Type {
	if input < 0 {
		input = 0
	}
	if output < 0 {
		output = 0
	}

	return Type{input: input, output: output, counted: true}
}
