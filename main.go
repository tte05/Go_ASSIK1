package main

import (
	"context"
	"encoding/json"
	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"math"
	"net/http"
	"os"
	"strconv"
)

type Game struct {
	ID          primitive.ObjectID `bson:"_id,omitempty"`
	Title       string             `bson:"title" json:"title"`
	Genre       string             `bson:"genre" json:"genre"`
	Rating      float64            `bson:"rating,omitempty" json:"rating,omitempty"`
	Developer   string             `bson:"developer,omitempty" json:"developer,omitempty"`
	Description string             `bson:"description,omitempty" json:"description,omitempty"`
}

const (
	connectionString = "mongodb://localhost:27017"
	dbName           = "gamesdb"
	collectionName   = "games"
	logFilePath      = "app.log"
)

var collection *mongo.Collection
var logger *logrus.Logger

func main() {
	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		panic("Unable to create log file: " + err.Error())
	}
	defer logFile.Close()

	logger = logrus.New()
	logger.Out = logFile

	clientOptions := options.Client().ApplyURI(connectionString)
	client, err := mongo.Connect(context.Background(), clientOptions)
	if err != nil {
		logger.Fatal("Error connecting to MongoDB:", err)
	}

	err = client.Ping(context.Background(), nil)
	if err != nil {
		logger.Fatal("Error pinging MongoDB:", err)
	}
	logger.Info("Connected to MongoDB!")

	collection = client.Database(dbName).Collection(collectionName)

	router := mux.NewRouter()

	router.HandleFunc("/games", getGamesHandler).Methods("GET")
	router.HandleFunc("/games", addGameHandler).Methods("POST")
	router.HandleFunc("/games/{id}", getGameByIDHandler).Methods("GET")
	router.HandleFunc("/games/{id}", updateGameByIDHandler).Methods("PUT")
	router.HandleFunc("/games/{id}", deleteGameByIDHandler).Methods("DELETE")

	fs := http.FileServer(http.Dir("templates"))
	router.PathPrefix("/").Handler(fs)

	logger.Info("Server started at localhost:8080")
	logger.Fatal(http.ListenAndServe(":8080", router))
}

func getGamesHandler(w http.ResponseWriter, r *http.Request) {
	sortBy := r.URL.Query().Get("sortBy")
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	pageSize, err := strconv.Atoi(r.URL.Query().Get("pageSize"))
	if err != nil || pageSize < 1 {
		pageSize = 5
	}

	offset := (page - 1) * pageSize

	sortOptions := bson.M{}
	switch sortBy {
	case "titleASC":
		sortOptions["title"] = 1
	case "genreASC":
		sortOptions["genre"] = 1
	case "ratingASC":
		sortOptions["rating"] = 1
	case "titleDESC":
		sortOptions["rating"] = -1
	case "genreDESC":
		sortOptions["rating"] = -1
	case "ratingDESC":
		sortOptions["rating"] = -1
	}

	options := options.Find().SetSort(sortOptions).SetSkip(int64(offset)).SetLimit(int64(pageSize))
	filter := bson.M{}
	minRating, err := strconv.ParseFloat(r.URL.Query().Get("minRating"), 64)
	if err == nil {
		filter["rating"] = bson.M{"$gte": minRating}
	}
	cursor, err := collection.Find(context.Background(), filter, options)

	if err != nil {
		handleError(w, err)
		return
	}
	defer cursor.Close(context.Background())

	var games []Game
	for cursor.Next(context.Background()) {
		var game Game
		if err := cursor.Decode(&game); err != nil {
			handleError(w, err)
			return
		}
		games = append(games, game)
	}

	totalGames, err := collection.CountDocuments(context.Background(), filter)
	if err != nil {
		handleError(w, err)
		return
	}
	totalPages := int(math.Ceil(float64(totalGames) / float64(pageSize)))

	responseData := map[string]interface{}{
		"games":      games,
		"totalPages": totalPages,
		"sortBy":     sortBy,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(responseData); err != nil {
		handleError(w, err)
		return
	}
}

func addGameHandler(w http.ResponseWriter, r *http.Request) {
	var game Game
	if err := json.NewDecoder(r.Body).Decode(&game); err != nil {
		handleError(w, err)
		return
	}

	if _, err := collection.InsertOne(context.Background(), game); err != nil {
		handleError(w, err)
		return
	}

	logger.WithFields(logrus.Fields{
		"title":       game.Title,
		"genre":       game.Genre,
		"rating":      game.Rating,
		"developer":   game.Developer,
		"description": game.Description,
	}).Info("New game added")

	w.WriteHeader(http.StatusCreated)
	response := map[string]string{"message": "Game added successfully"}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		handleError(w, err)
		return
	}
}

func getGameByIDHandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Path[len("/games/"):]
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		handleError(w, err)
		return
	}
	var game Game
	err = collection.FindOne(context.Background(), bson.M{"_id": objID}).Decode(&game)
	if err != nil {
		handleError(w, err)
		return
	}
	json.NewEncoder(w).Encode(game)
}

func updateGameByIDHandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Path[len("/games/"):]
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		handleError(w, err)
		return
	}
	var updatedGame Game
	err = json.NewDecoder(r.Body).Decode(&updatedGame)
	if err != nil {
		handleError(w, err)
		return
	}
	_, err = collection.ReplaceOne(context.Background(), bson.M{"_id": objID}, updatedGame)
	if err != nil {
		handleError(w, err)
		return
	}
	json.NewEncoder(w).Encode("Game updated successfully")
}

func deleteGameByIDHandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Path[len("/games/"):]
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		handleError(w, err)
		return
	}
	_, err = collection.DeleteOne(context.Background(), bson.M{"_id": objID})
	if err != nil {
		handleError(w, err)
		return
	}
	json.NewEncoder(w).Encode("Game deleted successfully")
}

func handleError(w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusInternalServerError)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
