package main

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path"
	"reflect"
	"runtime"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

const _10M = (1 << 20) * 10

type orderInfo struct {
	number      string
	name        string
	institution string
	email       string
}

func (o orderInfo) isValid() bool {
	if o.number == "" {
		return false
	}
	if o.name == "" {
		return false
	}
	if o.institution == "" {
		return false
	}
	if o.email == "" {
		return false
	}
	return true
}

type sizer interface {
	Size() int64
}

func randomDestinationPath() (string, string) {
	// Using a fixed seed will produce the same output on every run.
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))

	y, m, d := time.Now().Date()
	rndpart := fmt.Sprintf("20-%09d", rnd.Uint32())[0:12]
	outPath := fmt.Sprintf("%d%02d%02d_%s_ab1", y, m, d, rndpart)
	return rndpart, outPath
}

func createDestinationDir(uploadDir string) (string, string, error) {

	for i := 0; i < 10; i++ {
		orderNum, dest := randomDestinationPath()
		dirPath := uploadDir + string(os.PathSeparator) + dest
		//fmt.Println("about to create:", dirPath)
		if _, err := os.Stat(dirPath); os.IsNotExist(err) {
			//fmt.Println("dest:", dest)
			err := os.Mkdir(dirPath, 0755)
			if err == nil {
				return orderNum, dest, nil
			}
		}
	}

	return "", "", errors.New("Can't build destination")
}

func dbConn() (db *sql.DB) {
	dbDriver := "mysql"
	dbUser := os.Getenv("DB_USER")
	if dbUser == "" {
		panic(errors.New("DB user not defined"))
	}
	dbPass := os.Getenv("DB_PASS")
	if dbPass == "" {
		panic(errors.New("DB password not defined"))
	}
	dbHost := os.Getenv("DB_HOST")
	if dbHost == "" {
		panic(errors.New("DB host not defined"))
	}
	dbName := os.Getenv("DB_DB")
	if dbName == "" {
		panic(errors.New("mysql database not defined"))
	}
	db, err := sql.Open(dbDriver, dbUser+":"+dbPass+"@tcp("+dbHost+")/"+dbName)
	if err != nil {
		panic(err.Error())
	}
	err = db.Ping()
	if err != nil {
		panic(err)
	}

	return db
}

func storeData(order orderInfo, processedFiles []string) bool {
	db := dbConn()
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		log.Fatal(err)
	}

	// insert order
	stmt, err := tx.Prepare("INSERT INTO orders(number, name, email, institution, date_created) VALUES(?, ?, ?, ?, now())")
	if err != nil {
		log.Fatal(err)
	}
	res, err := stmt.Exec(order.number, order.name, order.institution, order.email)
	if err != nil {
		log.Fatal(err)
	}
	lastID, err := res.LastInsertId()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("last id: ", lastID)

	stmt, err = tx.Prepare("INSERT INTO order_files(order_id, file) VALUES(?, ?)")
	if err != nil {
		log.Fatal(err)
	}
	for _, filePath := range processedFiles {
		fmt.Println(" --  adding file: ", filePath)
		_, err := stmt.Exec(lastID, filePath)
		if err != nil {
			log.Fatal(err)
		}
	}

	tx.Commit()

	return true
}

func process(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-type", "text/html")
	if err := r.ParseMultipartForm(_10M); nil != err {
		//http.Error(w, err.Error(), http.StatusInternalServerError)
		fmt.Fprintf(w, "invalid request")
		return
	}

	uploadDir := os.Getenv("UPLOAD_DIR")
	if uploadDir == "" {
		panic(errors.New("upload dir was not defined"))
		return
	}

	//fmt.Fprintln(w, r.MultipartForm)
	fmt.Fprintf(w, "<div>name: %s</div>", r.FormValue("name"))
	fmt.Fprintf(w, "email: %s\n", r.FormValue("email"))
	fmt.Fprintf(w, "<div>institution: %s</div>", r.FormValue("institution"))

	orderNum, orderDir, err := createDestinationDir(uploadDir)
	if err != nil {
		fmt.Println("<div>Error: ", err, "</div>")
		fmt.Println("<div>Go back and try again..</div>")
		return
	}
	destDir := uploadDir + string(os.PathSeparator) + orderDir
	fmt.Printf("orderNum:%s\n", orderNum)
	fmt.Printf("finalDest:%s\n", destDir)

	order := orderInfo{
		number:      orderNum,
		name:        strings.TrimSpace(r.FormValue("name")),
		institution: strings.TrimSpace(r.FormValue("institution")),
		email:       strings.TrimSpace(r.FormValue("email")),
	}
	if !order.isValid() {
		fmt.Fprintln(w, "Missing info. <a href=\"javascript:history.go(-1)\">go back</a> and try again!")
		return
	}

	fmt.Printf("%v\n", order)
	fmt.Printf("%+v\n", order)

	processedFiles := make([]string, 0, 50)

	fmt.Fprintln(w, "---------------------\n")
	files := r.MultipartForm.File["input"]
	for i := range files { //Iterate over multiple uploaded files

		ext := path.Ext(files[i].Filename)
		fmt.Fprintf(w, "<div> + adding file: %s</div>", files[i].Filename)
		if ext != ".ab1" {
			fmt.Fprintf(w, "<div>  - skipping file: %s. Invalid extension %s</div>\n", files[i].Filename, ext)
			continue
		}

		file, err := files[i].Open()
		defer file.Close()
		if err != nil {
			fmt.Println("error reading file ", err)
			continue
		}

		//create destination file making sure the path is writeable.
		filePath := orderDir + string(os.PathSeparator) + files[i].Filename
		fullFilePath := uploadDir + string(os.PathSeparator) + filePath
		dst, err := os.Create(fullFilePath)
		defer dst.Close()
		if err != nil {
			fmt.Println("error creating destination ", err)
			return
		}
		//copy the uploaded file to the destination file
		if _, err := io.Copy(dst, file); err != nil {
			fmt.Println("error copying file", err)
			return
		}

		processedFiles = append(processedFiles, filePath)
	}

	ok := storeData(order, processedFiles)
	fmt.Printf("ok := %v\n\n\n", ok)
	if ok {
		fmt.Fprintf(w, "<div><br><br>Added %d files to GW order <strong>%s</strong></div>", len(processedFiles), order.number)
	}
}

func index(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "multipleUploads.html")
}

func xlog(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := runtime.FuncForPC(reflect.ValueOf(h).Pointer()).Name()
		fmt.Printf(" - uri:%s\thandler:%s\n", r.RequestURI, name)
		h(w, r)
	}
}

func main() {

	host := os.Getenv("HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Output(1, "starting server on "+host+":"+port)
	server := http.Server{
		Addr: host + ":" + port,
	}
	http.HandleFunc("/gwup/process", xlog(process))
	http.HandleFunc("/gwup", xlog(index))
	server.ListenAndServe()
}
