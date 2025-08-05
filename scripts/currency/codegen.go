package main

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"go/format"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

type currency struct {
	Name  string
	Code  string
	Num   string
	Scale string
}

func main() {
	if err := UpdateCurrencyData(); err != nil {
		panic(fmt.Errorf("error updating currency data: %v", err))
	}

	// Open the input file and read its contents
	data, err := readCsvFile(filepath.Join("scripts", "currency", "currency_data.csv"))
	if err != nil {
		panic(fmt.Errorf("error reading CSV file: %v", err))
	}

	// Convert the CSV records to a list of Currency objects
	currs := convertDataToCurrencies(data)

	// Generate Go code from the Currency objects using a template
	code, err := generateGoCode(filepath.Join("scripts", "currency", "currency_data.tmpl"), currs)
	if err != nil {
		panic(fmt.Errorf("error generating Go code: %v", err))
	}

	// Write the generated Go code to a file
	err = writeToFile("currency_data.go", code)
	if err != nil {
		panic(fmt.Errorf("error writing to file: %v", err))
	}
}

func readCsvFile(filename string) ([][]string, error) {
	// Open the CSV file
	in, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func() { _ = in.Close() }()

	// Read the CSV records
	reader := csv.NewReader(in)
	_, err = reader.Read() // header
	if err != nil {
		return nil, err
	}
	recs, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	return recs, nil
}

func convertDataToCurrencies(data [][]string) []currency {
	// Sort the CSV records by currency code
	less := func(i, j int) bool {
		a := data[i][1]
		b := data[j][1]
		switch a {
		case "XXX":
			return true
		case "XTS":
			return true
		}
		return a < b
	}
	sort.Slice(data, less)

	// Convert the CSV records to Currency objects
	currs := []currency{}
	for _, rec := range data {
		curr := currency{
			Name:  rec[0],
			Code:  rec[1],
			Num:   rec[2],
			Scale: rec[3],
		}
		currs = append(currs, curr)
	}
	return currs
}

func generateGoCode(filename string, currs []currency) ([]byte, error) {
	// Create a new template object from the template file
	fmap := template.FuncMap{
		"lower": strings.ToLower,
	}
	tmpl, err := template.New(filepath.Base(filename)).Funcs(fmap).ParseFiles(filename)
	if err != nil {
		return nil, err
	}

	// Execute the template
	var output bytes.Buffer
	err = tmpl.Execute(&output, currs)
	if err != nil {
		return nil, err
	}

	// Format the output as Go code
	formatted, err := format.Source(output.Bytes())
	if err != nil {
		return nil, err
	}
	return formatted, nil
}

func writeToFile(filename string, content []byte) error {
	// Write the content to a file
	out, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	writer := bufio.NewWriter(out)
	_, err = writer.Write(content)
	if err != nil {
		return err
	}
	err = writer.Flush()
	if err != nil {
		return err
	}
	return nil
}

// XML structures for parsing ISO 4217 currency data
type ISO4217 struct {
	CurrencyTable CurrencyTable `xml:"CcyTbl"`
}

type CurrencyTable struct {
	Entries []CurrencyEntry `xml:"CcyNtry"`
}

type CurrencyEntry struct {
	CountryName    string `xml:"CtryNm"`
	CurrencyName   string `xml:"CcyNm"`
	CurrencyCode   string `xml:"Ccy"`
	CurrencyNumber string `xml:"CcyNbr"`
	MinorUnits     string `xml:"CcyMnrUnts"`
}

// UpdateCurrencyData downloads the latest ISO 4217 currency list and updates currency_data.csv
func UpdateCurrencyData() error {
	// Download the XML file
	resp, err := http.Get("https://www.six-group.com/dam/download/financial-information/data-center/iso-currrency/lists/list-one.xml")
	if err != nil {
		return fmt.Errorf("failed to download XML: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download XML: status %d", resp.StatusCode)
	}

	// Read the XML data
	xmlData, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read XML data: %v", err)
	}

	// Parse the XML
	var iso4217 ISO4217
	err = xml.Unmarshal(xmlData, &iso4217)
	if err != nil {
		return fmt.Errorf("failed to parse XML: %v", err)
	}

	// Convert to our currency format and deduplicate
	currencyMap := make(map[string]currency)
	for _, entry := range iso4217.CurrencyTable.Entries {
		// Skip entries without currency codes
		if entry.CurrencyCode == "" {
			continue
		}

		// Convert minor units to scale (number of decimal places)
		scale := "2" // default scale
		if entry.MinorUnits != "" {
			if entry.MinorUnits == "N.A." {
				scale = "0"
			} else {
				// MinorUnits directly represents the number of decimal places
				scale = entry.MinorUnits
			}
		}

		// Use currency code as key to deduplicate
		currencyMap[entry.CurrencyCode] = currency{
			Name:  entry.CurrencyName,
			Code:  entry.CurrencyCode,
			Num:   entry.CurrencyNumber,
			Scale: scale,
		}
	}

	// Convert map to slice and sort
	var currencies []currency
	for _, curr := range currencyMap {
		currencies = append(currencies, curr)
	}

	// Sort currencies by code
	sort.Slice(currencies, func(i, j int) bool {
		a := currencies[i].Code
		b := currencies[j].Code
		// Keep special currencies at the end
		switch a {
		case "XXX":
			return false
		case "XTS":
			return b == "XXX"
		}
		switch b {
		case "XXX":
			return true
		case "XTS":
			return a != "XXX"
		}
		return a < b
	})

	// Write to CSV file
	csvPath := filepath.Join("scripts", "currency", "currency_data.csv")
	file, err := os.Create(csvPath)
	if err != nil {
		return fmt.Errorf("failed to create CSV file: %v", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	if err := writer.Write([]string{"Name", "Code", "Num", "Scale"}); err != nil {
		return fmt.Errorf("failed to write CSV header: %v", err)
	}

	// Write currency data
	for _, curr := range currencies {
		record := []string{curr.Name, curr.Code, curr.Num, curr.Scale}
		if err := writer.Write(record); err != nil {
			return fmt.Errorf("failed to write CSV record: %v", err)
		}
	}

	return nil
}
