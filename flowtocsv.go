package flowtocsv

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

//Instructions is an exported struct that contains the necessary instructions to convert an MRASCO flow into a CSV file.
//These instructions are stored as a JSON
type Instructions struct {
	Dataflow   string
	Delimiter  string
	DataItems  []string
	Spaces     []int
	Headers    []string
	Outputname string
	input      [][]string
}

var (
	headerFooter   [][]string
	output         [][]string
	chunk          [][]string
	replacedItems  []string
	spaceStrings   []string
	guide          []int
	matching       []int
	maxno          []int
	text           = ""
	prev           = 0
	mainguideIndex = 0
)

func (in *Instructions) loadInstructions(filename string) {
	i := *in
	jsonFile, _ := ioutil.ReadFile("./flowcrunch_instructions/" + filename)
	_ = json.Unmarshal([]byte(jsonFile), &i)
	*in = i
}

//Init ensures the three folders that are necessary are created in same folder as the executable/main package
//flowcrunch_instructions - where JSON instructions are kept
//flowcrunch_inputfiles - where any dataflows that need to be converted are kept
//flowcrunch_outputfiles - where any generated CSVs are placed
//init also clears any files contained within the 'flowcrunch_outputfiles' folder.
func (i Instructions) Init() {
	ensureDir("flowcrunch_learn")
	ensureDir("flowcrunch_instructions")
	ensureDir("flowcrunch_inputfiles")
	ensureDir("flowcrunch_outputfiles")
	deleteall("flowcrunch_outputfiles")
}

//Start begins the conversion of energy industry dataflows into CSV files.
//Before using start, you'll want to use the Learn function.
func (i Instructions) Start() {
	instructions, err := ioutil.ReadDir("./flowcrunch_instructions")
	if err != nil {
		fmt.Println(err)
	}
	inputfiles, err := ioutil.ReadDir("./flowcrunch_inputfiles")
	if err != nil {
		fmt.Println(err)
	}
	for _, instructionfile := range instructions {
		i.loadInstructions(instructionfile.Name())
		i.writeTo(i.Outputname, true)
		for _, inputfile := range inputfiles {
			i.Convert("./flowcrunch_inputfiles/" + inputfile.Name())
		}
	}
	fmt.Println("Complete")
}

//Learn takes in all dataflows with no duplicate items but at least one of every significant item so that it can learn the dataflow structure for converting to CSV
//Then it saves the instructions to a JSON file saved in a folder named 'flowcrunch_instructions'.
//All you need to do is save energy indsutry dataflows with the filename as the dataflow identifier.
//Also at the very beginning of the file, you need to insert the delimiter i.e a command or a pipe.
func (i Instructions) Learn() {
	learnfolder, err := ioutil.ReadDir("./flowcrunch_learn")
	if err != nil {
		fmt.Println(err)
	}
	for _, learnfile := range learnfolder {
		i.learning("./flowcrunch_learn/" + learnfile.Name())
	}
}

func (in *Instructions) learning(filename string) {
	i := *in
	i.Dataflow = ""
	i.Delimiter = ""
	i.DataItems = []string{}
	i.Spaces = []int{}
	i.Headers = []string{}
	i.Outputname = ""
	i.input = [][]string{}
	output = [][]string{}
	columns := []int{}
	content, err := ioutil.ReadFile(filename)
	endname := strings.Split(filename, "/")
	i.Outputname = strings.Split(endname[len(endname)-1], ".")[0] + "_Converted"
	i.Dataflow = strings.Split(endname[len(endname)-1], ".")[0]
	i.Delimiter = string(content[0])
	if err != nil {
		log.Fatal(err)
	}
	s := strings.Split(string(content), "\n")
	for index := range s {
		if containsrune(s[index], i.Delimiter) == true {
			slice := strings.Split(s[index], i.Delimiter)
			for index := range slice {
				slice[index] = strings.Replace(slice[index], "\"", "", -1)
			}
			if index == 0 {
				columns = append(columns, len(slice))
			}
			if index != 0 && index != len(s)-1 {
				columns = append(columns, len(slice))
				i.DataItems = append(i.DataItems, slice[0])
				i.Spaces = append(i.Spaces, len(slice)-2)
			}
			if index == len(s)-1 {
				columns = append(columns, len(slice))
			}
		}
	}
	var y = 0
	for index := range columns {
		var x = 0
		for x = 0; x < columns[index]; x++ {
			y++
			i.Headers = append(i.Headers, i.Dataflow+"_COLUMN_"+strconv.Itoa(y))
		}
		i.Headers = append(i.Headers, i.Dataflow+"_COLUMN_"+strconv.Itoa(y))
	}
	//i.DataItems = i.DataItems[:len(i.DataItems)-1]
	save(i)
	*in = i
}

//Convert begins the process of converting the dataflow into a CSV file - depending on what instruction has been loaded.
//Usually you just need to use 'Start' to convert all dataflows, but this is exported so you can target one file if necessary.
func (in *Instructions) Convert(filename string) {
	i := *in
	headerFooter = [][]string{}
	replacedItems = []string{}
	spaceStrings = []string{}
	guide = []int{}
	text = ""
	checking := [][]string{}
	i.input = [][]string{}
	f, _ := os.Open(filename)
	r := csv.NewReader(f)
	r.Comma = rune(i.Delimiter[0])
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	checker := false
	count := 0
	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return
		}
		for index := range record {
			if index == 0 {
				checking = append(checking, record)
				for a := range checking {
					for b := range checking[a] {
						if b < 4 {
							if strings.Contains(checking[a][b], i.Dataflow) == true {
								checker = true
							}
						}
					}
				}
			}
			record[index] = strings.Replace(record[index], "\"", "", -1)
			record[index] = strings.Replace(record[index], ",", "", -1)
		}
		if checker == true {
			if count != 0 {
				i.input = append(i.input, record)
			} else {
				headerFooter = append(headerFooter, record)
			}
		}
		count++
	}
	if checker == true {
		headerFooter = append(headerFooter, i.input[len(i.input)-1])
		i.input = i.input[:len(i.input)-1]
		*in = i
		i.replace()
	}
}

func (in *Instructions) replace() {
	i := *in
	for dataindex := range i.DataItems {
		temp := strings.Split(i.DataItems[dataindex], string(i.Delimiter))
		replacedItems = append(replacedItems, temp[0]+"_crunched")
	}
	for dataindex := range i.input {
		for itemindex := range i.DataItems {
			if i.input[dataindex][0] == i.DataItems[itemindex] {
				i.input[dataindex][0] = replacedItems[itemindex]
			}
		}
	}
	for spaceindex := range i.Spaces {
		spacetext := ""
		count := 0
		for count < i.Spaces[spaceindex] {
			spacetext += i.Delimiter
			count++
		}
		spaceStrings = append(spaceStrings, spacetext)
	}
	*in = i
	i.collate()
}

func (i Instructions) collate() {
	if len(i.input) != 0 {
		for InputIndex := range i.input {
			for replacedItemsIndex := range replacedItems {
				if len(chunk) != 0 {
					if i.input[InputIndex][0] == replacedItems[0] {
						i.iterator()
						guide = []int{}
						chunk = [][]string{}
						chunk = append(chunk, i.input[InputIndex])
						guide = append(guide, replacedItemsIndex)
						break
					} else if i.input[InputIndex][0] == replacedItems[replacedItemsIndex] {
						chunk = append(chunk, i.input[InputIndex])
						guide = append(guide, replacedItemsIndex)
						break
					}
				} else {
					if i.input[InputIndex][0] == replacedItems[0] {
						chunk = append(chunk, i.input[InputIndex])
						guide = append(guide, replacedItemsIndex)
						break
					}
				}
			}
		}
		i.iterator()
		guide = []int{}
		chunk = [][]string{}
	}
}

func (i Instructions) iterator() {
	var maximum = 0
	for head := 1; head <= len(guide)-1; head++ {
		one := icount(guide, guide[head-1])
		two := icount(guide, guide[head])
		if two >= one {
			maximum = two
		}
	}
	for maximum > 0 {
		text = ""
		i.crunch()
		maximum--
	}
	i.writeTo(i.Outputname, false)
	output = [][]string{}
}

func (i Instructions) crunch() {
	maxno = []int{}
	matching = []int{}
	prev = 0
	mainguideIndex = 0
	for guideIndex := range guide {
		maxno = append(maxno, icount(guide, guide[guideIndex]))
	}
	for mainguideIndex < len(guide) {
		text = i.chew(text, mainguideIndex)
		mainguideIndex++
	}
	i.fill()
}

func (i Instructions) chew(text string, mainguideIndex int) string {
	if text == "" && guide[mainguideIndex] == 0 {
		text += strings.Join(chunk[mainguideIndex], i.Delimiter) + i.Delimiter
		matching = append(matching, guide[mainguideIndex])
		prev = guide[mainguideIndex]
	} else if text != "" && guide[mainguideIndex] != 0 && guide[mainguideIndex] > prev {
		if contains(matching, guide[mainguideIndex]) == false {
			if icount(guide, guide[mainguideIndex]) > 1 {
				text += strings.Join(chunk[mainguideIndex], i.Delimiter) + i.Delimiter
				matching = append(matching, guide[mainguideIndex])
				prev = guide[mainguideIndex]
				chunk = remove2D(chunk, mainguideIndex)
				guide = remove1D(guide, mainguideIndex)
				text = i.chew(text, mainguideIndex)
			} else if icount(guide, guide[mainguideIndex]) == 1 {
				if maxno[mainguideIndex] == 1 {
					text += strings.Join(chunk[mainguideIndex], i.Delimiter) + i.Delimiter
					matching = append(matching, guide[mainguideIndex])
					prev = guide[mainguideIndex]
					text = i.chew(text, mainguideIndex)
				} else if maxno[mainguideIndex] > 1 {
					text += strings.Join(chunk[mainguideIndex], i.Delimiter) + i.Delimiter
					matching = append(matching, guide[mainguideIndex])
					prev = guide[mainguideIndex]
					mainguideIndex--
					text = i.chew(text, mainguideIndex)
				}
			}
		}
	}
	return text
}

func (i Instructions) fill() {
	ReverseIndex := len(replacedItems) - 1
	for ReverseIndex >= 0 {
		if strings.Contains(text, replacedItems[ReverseIndex]) == false {
			if ReverseIndex == len(replacedItems)-1 {
				text = text + replacedItems[ReverseIndex] + i.Delimiter + spaceStrings[ReverseIndex]
				if validate(text) == true {
					i.complete()
				}
			}
			if ReverseIndex == len(replacedItems)-2 {
				text = text + replacedItems[ReverseIndex] + i.Delimiter + spaceStrings[ReverseIndex]
				if validate(text) == true {
					i.complete()
				}
			}
		}
		if strings.Contains(text, replacedItems[ReverseIndex]) == true {
			splitex := strings.Split(text, replacedItems[ReverseIndex])
			if len(splitex) != 1 && ReverseIndex != 0 {
				if strings.Contains(text, replacedItems[ReverseIndex-1]) == false {
					if ReverseIndex-1 == 0 {
						text = replacedItems[ReverseIndex-1] + spaceStrings[ReverseIndex-1] + i.Delimiter + replacedItems[ReverseIndex] + splitex[1]
						if validate(text) == true {
							i.complete()
						}
					} else {
						text = splitex[0] + replacedItems[ReverseIndex-1] + spaceStrings[ReverseIndex-1] + i.Delimiter + replacedItems[ReverseIndex] + splitex[1]
						if validate(text) == true {
							i.complete()
						}
					}
				}
			}
		}
		ReverseIndex--
	}
	i.complete()
}

func (i Instructions) complete() {
	text = strings.Join(headerFooter[0], ",") + i.Delimiter + text + i.Delimiter + strings.Join(headerFooter[1], ",")
	text = strings.Replace(text, i.Delimiter, ",", -1)
	output = append(output, strings.Split(text, ","))
}

func (i Instructions) writeTo(filename string, boolean bool) {
	filepath := "./flowcrunch_outputfiles/"
	//os.Create(filepath + filename + ".csv")
	csvFile, err := os.OpenFile(filepath+filename+".csv", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed creating file: %s", err)
	}
	csvwriter := csv.NewWriter(csvFile)
	if boolean == false {
		for index := range output {
			output[index] = append(output[index], strconv.Itoa(index))
			csvwriter.Write(output[index])
		}
	} else {
		csvwriter.Write(i.Headers)
	}
	if err := csvwriter.Error(); err != nil {
		log.Fatalln("error writing csv:", err)
	}
	csvwriter.Flush()
	csvFile.Close()
}

func ensureDir(dirName string) error {
	err := os.MkdirAll(dirName, os.ModeDir)
	if err == nil || os.IsExist(err) {
		return nil
	} else {
		return err
	}
}

func save(i Instructions) {
	tobesaved := &i
	output, err := json.MarshalIndent(tobesaved, "", "\t")
	if err != nil {
		fmt.Println(err)
		return
	}
	_ = ioutil.WriteFile("./flowcrunch_instructions/"+i.Dataflow+".json", output, 0755)
	fmt.Println("Saved " + i.Dataflow + "!")
}

func contains(s []int, e int) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

func insert2D(slice *[][]string, tobeinserted []string) {
	s := *slice
	s = append(s, tobeinserted)
	*slice = s
}

func remove2D(slice [][]string, s int) [][]string {
	if s != len(slice) {
		slice = append(slice[:s], slice[s+1:]...)
		return slice
	} else if s == len(slice) {
		slice = slice[:len(slice)-1]
		return slice
	}
	return slice
}

func remove1D(slice []int, s int) []int {
	if s != len(slice) {
		slice = append(slice[:s], slice[s+1:]...)
		return slice
	} else if s == len(slice) {
		slice = slice[:len(slice)-1]
		return slice
	}
	return slice
}

func icount(input []int, item int) int {
	counter := 0
	for index := range input {
		if input[index] == item {
			counter++
		}
	}
	return counter
}

func containsrune(s string, e string) bool {
	for _, a := range s {
		if a == rune(e[0]) {
			return true
		}
	}
	return false
}

func validate(text string) bool {
	var validation = len(replacedItems)
	for index := range replacedItems {
		if strings.Contains(text, replacedItems[index]) == false {
			validation--
		}
	}
	if validation == len(replacedItems) {
		return true
	}
	return false
}

func deleteall(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	names, err := d.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, name := range names {
		err = os.RemoveAll(filepath.Join(dir, name))
		if err != nil {
			return err
		}
	}
	return nil
}