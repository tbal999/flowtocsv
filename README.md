# flowtocsv / flowcrunch
This is an MRASCO ETL library that can convert MRASCO flows into CSV files.
Then you can load them into SQL to do automation / analysis.

FOR: Data analysts in small energy industry retail firms who want to develop BI around energy industry data i.e gas and electricity data but do not have the capability to automate the parsing of MRASCO dataflows into their servers.

USE CASE: You want to monitor read disputes actively, tracking when they are raised, and when they are rejected etc, but you don't want to hire a contractor to build you a workflow for £££/$$$. 
1) Ensure you have SFTP server (or API) up and running which gives you access to daily incoming D0300 dataflows
2) Set up pipeline to download all D0300 dataflows daily to server to a flowcrunch_inputfiles folder
3) Set up flowtocsv so that it can parse D0300 dataflows into CSV format
4) Upload CSV file into SQL using staging/production tables & merge methodology
5) Use Tableau / Power BI / Qlik to load table into view and join necessray tables (will require energy industry knowledge!)

You've got an up and running BI pipeline that keeps track of read disputes and whether they are successful/rejected/ignored etc etc.

The number of use cases for this tool is AT LEAST the same as the number of UK energy industry dataflows that exist for suppliers/retailers (128). Each of varying significance and impact.

Docs:
https://pkg.go.dev/github.com/tbal999/flowtocsv

This particular library can also handle other flat file types. My stance would be to try it out. If it works, great!
Through doing this I discovered new ways how to wrangle data using recursion in go. This library can be inserted into ETL packaged solutions.

At the moment there are a few workflows for this library.

1) SFTP server -> download data flows to a folder -> parse data flows into csv -> load into SQL.
2) Connect to MRASCO SQL server or API -> load clobs into Go as strings -> parse clobs into csv -> load into SQL.

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
		"150_COLUMN_5",
		"150_COLUMN_6",
		"150_COLUMN_7",
		"150_COLUMN_8",
		"150_COLUMN_9",
		"150_COLUMN_10",
		"150_COLUMN_11",
		"150_COLUMN_12",
		"150_COLUMN_13",
		"150_COLUMN_13",
		"150_COLUMN_14",
		"150_COLUMN_15",
		"150_COLUMN_16",
		"150_COLUMN_17",
		"150_COLUMN_18",
		"150_COLUMN_19",
		"150_COLUMN_19",
		"150_COLUMN_20",
		"150_COLUMN_21",
		"150_COLUMN_22",
		"150_COLUMN_23",
		"150_COLUMN_24",
		"150_COLUMN_25",
		"150_COLUMN_25",
		"150_COLUMN_26",
		"150_COLUMN_27",
		"150_COLUMN_28",
		"150_COLUMN_29",
		"150_COLUMN_30",
		"150_COLUMN_31",
		"150_COLUMN_32",
		"150_COLUMN_33",
		"150_COLUMN_34",
		"150_COLUMN_35",
		"150_COLUMN_36",
		"150_COLUMN_37",
		"150_COLUMN_38",
		"150_COLUMN_39",
		"150_COLUMN_40",
		"150_COLUMN_41",
		"150_COLUMN_42",
		"150_COLUMN_43",
		"150_COLUMN_44",
		"150_COLUMN_45",
		"150_COLUMN_46",
		"150_COLUMN_47",
		"150_COLUMN_48",
		"150_COLUMN_49",
		"150_COLUMN_50",
		"150_COLUMN_51",
		"150_COLUMN_52",
		"150_COLUMN_52",
		"150_COLUMN_53",
		"150_COLUMN_54",
		"150_COLUMN_55",
		"150_COLUMN_56",
		"150_COLUMN_57",
		"150_COLUMN_58",
		"150_COLUMN_59",
		"150_COLUMN_60",
		"150_COLUMN_61",
		"150_COLUMN_62",
		"150_COLUMN_62",
		"150_COLUMN_63",
		"150_COLUMN_64",
		"150_COLUMN_65",
		"150_COLUMN_66",
		"150_COLUMN_67",
		"150_COLUMN_68",
		"150_COLUMN_69",
		"150_COLUMN_70",
		"150_COLUMN_71",
		"150_COLUMN_72",
		"150_COLUMN_72",
		"150_COLUMN_73",
		"150_COLUMN_74",
		"150_COLUMN_75",
		"150_COLUMN_76",
		"150_COLUMN_77",
		"150_COLUMN_77",
		"150_COLUMN_78",
		"150_COLUMN_79",
		"150_COLUMN_80",
		"150_COLUMN_81",
		"150_COLUMN_82",
		"150_COLUMN_83",
		"150_COLUMN_84",
		"150_COLUMN_84"
	],
	"Outputname": "150_Converted"
}
```
Then you can place as many D0150 dataflows as you want in the 'inputfiles' folder and it will create an aggregated CSV file of all those dataflows.
Instructions can sometimes add too many columns, so just remove some from the JSON if necessary.

This methodology works for nearly all UK energy industry data flows.


