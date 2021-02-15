# flowtocsv
This is an MRASCO ETL library that can convert MRASCO flows into CSV files.
Then you can load them into SQL to do automation / analysis.

originally developed 27th Sep onwards.

Docs:
https://pkg.go.dev/github.com/tbal999/flowtocsv

After building many iterations of this type of tool in the past, decided to build a brand new one with different logic in my free time.

This particular library can also handle other flat file types. My stance would be to try it out. If it works, great!

Through doing this I discovered new ways how to wrangle data using recursion in go. This library can be inserted into ETL packaged solutions.

Example code:
```
package main

import (
	flow "github.com/tbal999/flowtocsv"
)

func main() {
	f := flow.Instructions{}
	f.Init()
	f.Learn("|") //f.Learn(",") - for Gas SPAA Dataflows.
	f.Start()
}
```
This will create four folders - flowcrunch_inputfiles, flowcrunch_instructions, flowcrunch_learn and flowcrunch_outputfiles.
Simply save an example dataflow i.e a D0150 in the 'learn' folder and it will generate the necessary instructions to parse that dataflow into a CSV format:
```
ZHV|99999|D0150001|M|LLLL|X|AAAA|12345||||TR01|
288|12345|20179568|D|x|
289|0158|20179568|Z||
290|XXXTEST011|||40|F|A2Z 5000|LLLL|||||||||||K|20179568|20179568|20200101|||H|20179568|
293|L|C|AI|9.00||9||DE|
293|N|C|AI|9.00||7|||
08A|KXXA 12387|20179568|LLLL|
ZPT|99999|6||1|12345|
```
Then you can place as many D0150 dataflows as you want in the 'inputfiles' folder and it will create an aggregated CSV file of all those dataflows.


