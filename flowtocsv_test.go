package flowtocsv

import (
	"reflect"
	"testing"
)

func TestLearningClob(t *testing.T) {
	Testing = true
	instructions := Instructions{}
	testclob := `ZHV|99999|D0150001|M|LBSL|X|NEEB|20179568182352||||TR01|
288|127227343996|20179568|D|x|
289|0158|20179568|Z||
290|XXXTEST011|||40|F|L&G CL27|LBSL|||||||||||K|20179568|20179568|20200101|||H|20179568|
293|L|C|AI|9.00||9||DE|
293|N|C|AI|9.00||7|||
08A|KXXA 12387|20179568|LBSL|
ZPT|99999|6||1|20179568182352|`
	instructions.LearningClob("D0150", testclob, "|")
	wantdataitems := []string{"288", "289", "290", "293", "08A"}
	gotdataitems := instructions.DataItems
	if !reflect.DeepEqual(gotdataitems, wantdataitems) {
		t.Errorf("got %q want %q", gotdataitems, wantdataitems)
	}
	wantspaces := []int{4, 4, 25, 8, 3}
	gotspaces := instructions.Spaces
	if !reflect.DeepEqual(gotspaces, wantspaces) {
		t.Errorf("got %q want %q", gotspaces, wantspaces)
	}
}

func TestConvertClob(t *testing.T) {
	Testing = true
	instructions := Instructions{
		Dataflow:   "D0150",
		Delimiter:  "|",
		DataItems:  []string{"288", "289", "290", "293", "08A"},
		Spaces:     []int{4, 4, 25, 8, 3},
		Headers:    []string{"HEADERS", "WOULD", "GO", "HERE"},
		Outputname: "D0150_Converted",
	}
	testclob := `ZHV|99999|D0150001|M|LBSL|X|NEEB|20179568182352||||TR01|
288|127227343996|20179568|D|x|
289|0158|20179568|Z||
290|XXXTEST011|||40|F|L&G CL27|LBSL|||||||||||K|20179568|20179568|20200101|||H|20179568|
293|L|C|AI|9.00||9||DE|
293|N|C|AI|9.00||7|||
08A|KXXA 12387|20179568|LBSL|
ZPT|99999|6||1|20179568182352|`
	got := instructions.ConvertClob(testclob)
	want := [][]string{{"ZHV", "99999", "D0150001", "M", "LBSL", "X", "NEEB", "20179568182352", "", "", "", "TR01", "", "288_crunched", "127227343996", "20179568", "D", "x", "", "289_crunched", "0158", "20179568", "Z", "", "", "290_crunched", "XXXTEST011", "", "", "40", "F", "L&G CL27", "LBSL", "", "", "", "", "", "", "", "", "", "", "K", "20179568", "20179568", "20200101", "", "", "H", "20179568", "", "293_crunched", "L", "C", "AI", "9.00", "", "9", "", "DE", "", "08A_crunched", "KXXA 12387", "20179568", "LBSL", "", "", "ZPT", "99999", "6", "", "1", "20179568182352", "", "", "testtime", "0", "0"},
		{"ZHV", "99999", "D0150001", "M", "LBSL", "X", "NEEB", "20179568182352", "", "", "", "TR01", "", "288_crunched", "127227343996", "20179568", "D", "x", "", "289_crunched", "0158", "20179568", "Z", "", "", "290_crunched", "XXXTEST011", "", "", "40", "F", "L&G CL27", "LBSL", "", "", "", "", "", "", "", "", "", "", "K", "20179568", "20179568", "20200101", "", "", "H", "20179568", "", "293_crunched", "N", "C", "AI", "9.00", "", "7", "", "", "", "08A_crunched", "KXXA 12387", "20179568", "LBSL", "", "", "ZPT", "99999", "6", "", "1", "20179568182352", "", "", "testtime", "1", "1"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q want %q", got, want)
	}
}
