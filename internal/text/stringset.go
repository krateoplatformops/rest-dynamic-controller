package text

type StringSet map[string]struct{}

func NewStringSet(strs ...string) StringSet {
	set := StringSet{}
	for _, str := range strs {
		set[str] = struct{}{}
	}
	return set
}

func (s StringSet) Add(str string) {
	s[str] = struct{}{}
}

func (s StringSet) Contains(str string) bool {
	_, exists := s[str]
	return exists
}

func (s StringSet) Remove(str string) {
	delete(s, str)
}
