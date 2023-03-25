# flowtocsv / flowcrunch
This is an MRASCO ETL library that can convert MRASCO flows into CSV files.
Then you can load them into SQL to do automation / analysis. UNDER MIT LICENSE.... 2021

<b>DOCUMENTATION:</b>
https://pkg.go.dev/github.com/tbal999/flowtocsv

<b>FOR:</b> Data analysts & BI Specialists in small energy industry retail firms who want to develop BI around energy industry data i.e gas and electricity data but do not have the capability/functionality to automate the parsing of MRASCO dataflows into their databases.

<b>EXAMPLE USE CASE:</b> You want to monitor read disputes actively, tracking when they are raised, and when they are rejected etc, but you don't want to hire an expensive contractor to build you a workflow. 
1) Ensure you have SFTP server (or API) up and running which gives you access to daily incoming D0300 dataflows
2) Set up pipeline to download all D0300 dataflows daily to server to a flowcrunch_inputfiles folder
3) Set up flowtocsv so that it can parse D0300 dataflows into CSV format
4) Upload CSV file into SQL using staging/production tables & merge methodology
5) Use Tableau / Power BI / Qlik to load table into view and join necessray tables (will require energy industry knowledge!)

You've got an up and running BI pipeline that keeps track of read disputes and whether they are successful/rejected/ignored etc etc.

The number of use cases for this tool is AT LEAST the same as the number of UK energy industry dataflows that exist for suppliers/retailers (128). Each of varying significance and impact.

This particular library can also handle other flat file types. My stance would be to try it out. If it works, great!
Through doing this I discovered new ways how to wrangle data using recursion in go. This library can be inserted into ETL packaged solutions.

At the moment there are a few workflows for this library.

1) SFTP server -> download data flows to a folder -> parse data flows into csv -> load into SQL.
2) API -> load clobs into Go as strings -> parse clobs into csv -> load into SQL.

<b>Example code:</b>
```
package main

import (
	flow "github.com/tbal999/flowtocsv"
)

func main() {
	f := flow.Instructions{}
	f.Init()
	f.LearnFile("|") //f.Learn(",") - for Gas SPAA Dataflows.
	f.StartFiles()
}
```
This will create four folders - flowcrunch_inputfiles, flowcrunch_instructions, flowcrunch_learn and flowcrunch_outputfiles.
Simply save an example dataflow i.e a D0150001 in the 'learn' folder and it will generate the necessary instructions to parse that dataflow into a CSV format:
```
a fake dataflow clob
ZHV|99999|D0150001|M|LLLL|X|AAAA|12345||||TR01|
288|12345|20179568|D|x|
289|0158|20179568|Z||
290|XXXTEST011|||40|F|A2Z 5000|LLLL|||||||||||K|20179568|20179568|20200101|||H|20179568|
293|L|C|AI|9.00||9||DE|
293|N|C|AI|9.00||7|||
08A|KXXA 12387|20179568|LLLL|
ZPT|99999|6||1|12345|
```
Then it will learn the structure of the dataflow and create a set of instructions in the 'instructions' folder:

```
{
	"Dataflow": "150",
	"Delimiter": "|",
	"DataItems": [
		"288",
		"289",
		"290",
		"293",
		"08A"
	],
	"Spaces": [
		4,
		4,
		25,
		8,
		3
	],
	"Headers": [
		"150_COLUMN_1",
		"150_COLUMN_2",
		"150_COLUMN_3",
		"150_COLUMN_4",
		...
		...
	],
	"Outputname": "150_Converted"
}
```
Then you can place as many D0150 dataflows as you want in the 'inputfiles' folder and it will create an aggregated CSV file of all those dataflows.
Instructions can sometimes add too many columns, so just remove some from the JSON if necessary.

This methodology works for nearly all UK energy industry data flows.


