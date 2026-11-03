package business

import "log/slog"

// UsageKind is the kind of work that consumed something: writing the Script, speaking it or crawling pages.
type UsageKind string

// UsageUnit is what a UsageItem counts.
type UsageUnit string

const (
	// UsageScript is the work of writing the Script.
	UsageScript UsageKind = "script"
	// UsageSpeech is the work of speaking the Script.
	UsageSpeech UsageKind = "speech"
	// UsageCrawl is the work of retrieving pages.
	UsageCrawl UsageKind = "crawl"
)

const (
	// UnitInputTokens are the tokens of the text handed to a model.
	UnitInputTokens UsageUnit = "input_tokens"
	// UnitOutputTokens are the tokens a model answered with, thinking included, as the service bills them.
	UnitOutputTokens UsageUnit = "output_tokens"
	// UnitAudioTokens are the tokens of the audio a model produced.
	UnitAudioTokens UsageUnit = "audio_tokens"
	// UnitPages are the pages a Crawler retrieved.
	UnitPages UsageUnit = "pages"
)

// UsageItem is how much of one unit one kind of work consumed.
type UsageItem struct {
	Kind     UsageKind
	Unit     UsageUnit
	Quantity int64
}

// Usage is what producing an Episode, or a step of it, consumed. It counts resources, not money. The zero value is
// no Usage.
type Usage []UsageItem

// Add returns the Usage of both together: items of equal kind and unit are summed, the others are kept, in the order
// they first appeared. Neither operand is changed.
func (u Usage) Add(other Usage) Usage {
	var sum Usage
	for _, items := range []Usage{u, other} {
		for _, item := range items {
			sum = sum.add(item)
		}
	}
	return sum
}

// LogValue shows the Usage in a log line as one attribute per kind and unit, for example script.input_tokens=45120.
func (u Usage) LogValue() slog.Value {
	attrs := make([]slog.Attr, 0, len(u))
	for _, item := range u {
		attrs = append(attrs, slog.Int64(string(item.Kind)+"."+string(item.Unit), item.Quantity))
	}
	return slog.GroupValue(attrs...)
}

// add returns the Usage with the item counted in.
func (u Usage) add(item UsageItem) Usage {
	for i := range u {
		if u[i].Kind == item.Kind && u[i].Unit == item.Unit {
			u[i].Quantity += item.Quantity
			return u
		}
	}
	return append(u, item)
}
