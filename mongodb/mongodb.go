package mongodb

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func HandleMongoError(err error, client *mongo.Client) {
	if err == nil {
		return
	}

	if strings.Contains(err.Error(), "client is disconnected") {
		fmt.Println("MongoDB connection error detected, attempting to reconnect...")

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		opts := options.Client().ApplyURI("mongodb://db:27017/")
		newClient, err := mongo.Connect(ctx, opts)
		if err != nil {
			fmt.Printf("Failed to reconnect to MongoDB: %v\n", err)
			os.Exit(1)
		}
		*client = *newClient

		fmt.Println("Successfully reconnected to MongoDB")
		return
	}

	fmt.Printf("MongoDB error: %v\n", err)
}
