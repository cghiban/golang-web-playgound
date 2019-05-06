package main

import (
	"database/sql"
	"errors"
	"fmt"
	"html/template"
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
	"github.com/gorilla/sessions"
)

var cStore *sessions.CookieStore

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
	res, err := stmt.Exec(order.number, order.name, order.email, order.institution)
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
	session, err := cStore.Get(r, "local-session")
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

	orderNum, orderDir, err := createDestinationDir(uploadDir)
	if err != nil {
		fmt.Println("Error creating destination dir: ", err)
		session.AddFlash("Can't create destination dir ")
		session.Save(r, w)
		http.Redirect(w, r, "/gwup", 302)
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
		session.AddFlash(fmt.Sprintf("Missing info"))
		session.Save(r, w)
		http.Redirect(w, r, "/gwup", 302)
	}

	processedFiles := make([]string, 0, 50)

	//fmt.Fprintln(w, "---------------------\n")
	files := r.MultipartForm.File["input"]
	for i := range files { //Iterate over multiple uploaded files

		ext := path.Ext(files[i].Filename)
		//fmt.Fprintf(w, "<div> + adding file: %s</div>", files[i].Filename)
		if ext != ".ab1" {
			session.AddFlash(fmt.Sprintf("skipping file: %s. Invalid extension %s\n", files[i].Filename, ext))
			continue
		}

		file, err := files[i].Open()
		defer file.Close()
		if err != nil {
			fmt.Println("error reading file ", err)
			session.AddFlash(fmt.Sprintf("Error reading file %s", files[i].Filename))
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

	if len(processedFiles) > 0 {
		fmt.Printf("about to add: %+v\n", order)
		ok := storeData(order, processedFiles)
		//fmt.Printf("ok := %v\n\n\n", ok)
		if ok {

			msg := fmt.Sprintf("Added %d files to fake GW order %s", len(processedFiles), order.number)
			session.AddFlash(msg)
		}
	} else {
		session.AddFlash("No files were uploaded!")
		// no need to keep an empty folder
		dirToRemove := uploadDir + string(os.PathSeparator) + orderDir
		if err := os.Remove(dirToRemove); err != nil {
			fmt.Println("removed empty dir: ", dirToRemove)
		}
	}
	session.Save(r, w)
	http.Redirect(w, r, "/gwup", 302)
}

func index(w http.ResponseWriter, r *http.Request) {
	session, err := cStore.Get(r, "local-session")
	session.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   3600 * 1, // one hour
		HttpOnly: true,
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	msg := ""
	if flashes := session.Flashes(); len(flashes) > 0 {
		for _, f := range flashes {
			msg += fmt.Sprintf("» %s", f)
		}
	}
	session.Save(r, w)
	// Create a new template and parse the data into it.
	t := template.Must(template.New("index").Parse(getTemplate()))
	t.ExecuteTemplate(w, "index", msg)
}

func xlog(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := runtime.FuncForPC(reflect.ValueOf(h).Pointer()).Name()
		fmt.Printf(" - uri:%s\thandler:%s\n", r.RequestURI, name)
		h(w, r)
	}
}

func getTemplate() string {
	return string([]byte(`
	<!DOCTYPE html>
	<html>
	<head>
	<meta charset="UTF-8" />
	</head>
	<body>
	{{ if . }}
	<pre>{{ . }}</pre>
	{{ end }}
	<div>
	<form method="post" action="/gwup/process" enctype="multipart/form-data">
		<label>Name</label><input name="name" type="text" value="" />
		<label>Institution</label><input name="institution" type="text" value="" />
		<label>Email</label><input name="email" type="email" value="" />
		<div>
			<label>Upload</label><input name="input" type="file" multiple/>
		</div>
		<input type="submit" value="submit" />
	</form>
	</div>
	</body>
	</html>`))
}

func main() {

	// init sesssion store
	cStore = sessions.NewCookieStore([]byte(os.Getenv("SESSION_KEY")))

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
	if err := server.ListenAndServe(); err != nil {
		fmt.Println("Error: ", err)
		os.Exit(1)
	}
}
