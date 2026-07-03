package core

func Apply(buffer string, edit TextEdit) string {
	if edit.Start < 0 {
		edit.Start = 0
	}
	if edit.End < edit.Start {
		edit.End = edit.Start
	}
	if edit.Start > len(buffer) {
		edit.Start = len(buffer)
	}
	if edit.End > len(buffer) {
		edit.End = len(buffer)
	}
	return buffer[:edit.Start] + edit.Text + buffer[edit.End:]
}
