package readonly

type Slice[T any] struct {
	ᚢ []T
}

func SliceOf[T any](x []T) Slice[T] {
	return Slice[T]{x}
}

func (v Slice[T]) Len() int { return len(v.ᚢ) }

func (v Slice[T]) At(i int) T { return v.ᚢ[i] }
