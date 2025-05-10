package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	calendar "google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

const (
	credentialsFile = "credentials.json"
	timeFormat      = "2006-01-02 15:04"
	timeZone        = "Asia/Kolkata" 
)

type Config struct {
	TokenPath string
}

func getTokenFilePath() (string, error) {
	usr, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("failed to get current user: %w", err)
	}
	return filepath.Join(usr.HomeDir, "token.json"), nil
}

func getClient(config *oauth2.Config) (*http.Client, error) {
	tokPath, err := getTokenFilePath()
	if err != nil {
		return nil, err
	}

	tok, err := tokenFromFile(tokPath)
	if err != nil {
		// No cached token, perform auth flow
		tok, err = getTokenFromWeb(config)
		if err != nil {
			return nil, err
		}
		if err := saveToken(tokPath, tok); err != nil {
			return nil, err
		}
	}

	return config.Client(context.Background(), tok), nil
}

func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

func getTokenFromWeb(config *oauth2.Config) (*oauth2.Token, error) {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the "+
		"authorization code: \n%v\n", authURL)
	
	code, err := promptForInput("Authorization code: ")
	if err != nil {
		return nil, fmt.Errorf("failed to read authorization code: %w", err)
	}

	tok, err := config.Exchange(context.Background(), code)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve token from web: %w", err)
	}
	return tok, nil
}

func saveToken(path string, token *oauth2.Token) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("unable to cache oauth token: %w", err)
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(token)
}

func promptForInput(msg string) (string, error) {
	fmt.Print(msg)
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(input), nil
}

func parseTime(input string) (string, error) {
	t, err := time.Parse(timeFormat, input)
	if err != nil {
		return "", fmt.Errorf("invalid time format (use YYYY-MM-DD HH:MM): %w", err)
	}
	return t.Format(time.RFC3339), nil
}

func listCalendars(srv *calendar.Service) ([]*calendar.CalendarListEntry, error) {
	calendarList, err := srv.CalendarList.List().Do()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve calendars: %w", err)
	}

	fmt.Println("\nAvailable calendars:")
	for i, cal := range calendarList.Items {
		fmt.Printf("%d) %s → %s\n", i+1, cal.Summary, cal.Id)
	}
	fmt.Println()
	
	return calendarList.Items, nil
}

func selectCalendar(srv *calendar.Service) (string, error) {
	calendars, err := listCalendars(srv)
	if err != nil {
		return "", err
	}
	
	if len(calendars) == 0 {
		return "", fmt.Errorf("no calendars found in your account")
	}
	
	calendarSelection, err := promptForInput("Enter calendar number or ID: ")
	if err != nil {
		return "", err
	}
	
	var calendarID string
	if index, err := fmt.Sscanf(calendarSelection, "%d", new(int)); err == nil && index > 0 {
		idx, _ := fmt.Sscanf(calendarSelection, "%d", new(int))
		if idx <= len(calendars) && idx > 0 {
			calendarID = calendars[idx-1].Id
		} else {
			return "", fmt.Errorf("invalid calendar number")
		}
	} else {
		calendarID = calendarSelection
	}
	
	return calendarID, nil
}

func listEvents(srv *calendar.Service, calendarID string) error {
	events, err := srv.Events.List(calendarID).
		MaxResults(10).
		OrderBy("startTime").
		SingleEvents(true).
		TimeMin(time.Now().Format(time.RFC3339)).
		Do()
	
	if err != nil {
		return fmt.Errorf("unable to retrieve events: %w", err)
	}
	
	if len(events.Items) == 0 {
		fmt.Println("No upcoming events found.")
		return nil
	}
	
	fmt.Println("\nUpcoming events:")
	for i, event := range events.Items {
		startTime := event.Start.DateTime
		if startTime == "" {
			startTime = event.Start.Date
		}
		
		endTime := event.End.DateTime
		if endTime == "" {
			endTime = event.End.Date
		}
		
		fmt.Printf("%d) %s (%s)\n   When: %s to %s\n", 
			i+1, event.Summary, event.Id, startTime, endTime)
	}
	fmt.Println()
	
	return nil
}

func createEvent(srv *calendar.Service, calendarID string) error {
	title, err := promptForInput("Title: ")
	if err != nil {
		return err
	}
	
	desc, err := promptForInput("Description: ")
	if err != nil {
		return err
	}
	
	startInput, err := promptForInput("Start (YYYY-MM-DD HH:MM): ")
	if err != nil {
		return err
	}
	
	startTime, err := parseTime(startInput)
	if err != nil {
		return err
	}
	
	endInput, err := promptForInput("End   (YYYY-MM-DD HH:MM): ")
	if err != nil {
		return err
	}
	
	endTime, err := parseTime(endInput)
	if err != nil {
		return err
	}
	
	start, _ := time.Parse(time.RFC3339, startTime)
	end, _ := time.Parse(time.RFC3339, endTime)
	if end.Before(start) {
		return fmt.Errorf("end time cannot be before start time")
	}
	
	event := &calendar.Event{
		Summary:     title,
		Description: desc,
		Start:       &calendar.EventDateTime{DateTime: startTime, TimeZone: timeZone},
		End:         &calendar.EventDateTime{DateTime: endTime, TimeZone: timeZone},
	}
	
	created, err := srv.Events.Insert(calendarID, event).Do()
	if err != nil {
		return fmt.Errorf("unable to create event: %w", err)
	}
	
	fmt.Printf("Event created successfully! ID: %s\n", created.Id)
	return nil
}

func getEvent(srv *calendar.Service, calendarID string) error {
	id, err := promptForInput("Event ID: ")
	if err != nil {
		return err
	}
	
	event, err := srv.Events.Get(calendarID, id).Do()
	if err != nil {
		return fmt.Errorf("unable to retrieve event: %w", err)
	}
	
	fmt.Printf("\nEvent Details:\n")
	fmt.Printf("Title: %s\n", event.Summary)
	fmt.Printf("Description: %s\n", event.Description)
	fmt.Printf("Start: %s\n", event.Start.DateTime)
	fmt.Printf("End: %s\n", event.End.DateTime)
	fmt.Printf("Status: %s\n", event.Status)
	if len(event.Attendees) > 0 {
		fmt.Println("Attendees:")
		for _, attendee := range event.Attendees {
			fmt.Printf(" - %s (%s)\n", attendee.Email, attendee.ResponseStatus)
		}
	}
	
	return nil
}

func updateEvent(srv *calendar.Service, calendarID string) error {
	id, err := promptForInput("Event ID: ")
	if err != nil {
		return err
	}
	
	event, err := srv.Events.Get(calendarID, id).Do()
	if err != nil {
		return fmt.Errorf("unable to retrieve event for update: %w", err)
	}
	
	fmt.Printf("Current title: %s\n", event.Summary)
	title, err := promptForInput("New title (leave empty to keep current): ")
	if err != nil {
		return err
	}
	if title != "" {
		event.Summary = title
	}
	
	fmt.Printf("Current description: %s\n", event.Description)
	desc, err := promptForInput("New description (leave empty to keep current): ")
	if err != nil {
		return err
	}
	if desc != "" {
		event.Description = desc
	}
	
	updateTime, err := promptForInput("Update time? (y/n): ")
	if err != nil {
		return err
	}
	
	if strings.ToLower(updateTime) == "y" {
		startInput, err := promptForInput(fmt.Sprintf("Start (YYYY-MM-DD HH:MM, current: %s): ", event.Start.DateTime))
		if err != nil {
			return err
		}
		
		if startInput != "" {
			startTime, err := parseTime(startInput)
			if err != nil {
				return err
			}
			event.Start.DateTime = startTime
		}
		
		endInput, err := promptForInput(fmt.Sprintf("End (YYYY-MM-DD HH:MM, current: %s): ", event.End.DateTime))
		if err != nil {
			return err
		}
		
		if endInput != "" {
			endTime, err := parseTime(endInput)
			if err != nil {
				return err
			}
			event.End.DateTime = endTime
		}
		
		start, _ := time.Parse(time.RFC3339, event.Start.DateTime)
		end, _ := time.Parse(time.RFC3339, event.End.DateTime)
		if end.Before(start) {
			return fmt.Errorf("end time cannot be before start time")
		}
	}
	
	updated, err := srv.Events.Update(calendarID, id, event).Do()
	if err != nil {
		return fmt.Errorf("unable to update event: %w", err)
	}
	
	fmt.Printf("Event updated successfully! ID: %s\n", updated.Id)
	return nil
}

func deleteEvent(srv *calendar.Service, calendarID string) error {
	id, err := promptForInput("Event ID to delete: ")
	if err != nil {
		return err
	}
	
	confirm, err := promptForInput(fmt.Sprintf("Are you sure you want to delete event %s? (y/n): ", id))
	if err != nil {
		return err
	}
	
	if strings.ToLower(confirm) != "y" {
		fmt.Println("Deletion cancelled.")
		return nil
	}
	
	err = srv.Events.Delete(calendarID, id).Do()
	if err != nil {
		return fmt.Errorf("unable to delete event: %w", err)
	}
	
	fmt.Println("Event deleted successfully.")
	return nil
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("Calendar CLI: ")
	
	b, err := os.ReadFile(credentialsFile)
	if err != nil {
		log.Fatalf("Unable to read credentials file (%s): %v", credentialsFile, err)
	}
	
	config, err := google.ConfigFromJSON(b, 
		calendar.CalendarScope,
		calendar.CalendarEventsScope,
		calendar.CalendarReadonlyScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}
	
	client, err := getClient(config)
	if err != nil {
		log.Fatalf("Unable to get OAuth client: %v", err)
	}
	
	srv, err := calendar.NewService(context.Background(), option.WithHTTPClient(client))
	if err != nil {
		log.Fatalf("Unable to create Calendar service: %v", err)
	}
	
	calendarID, err := selectCalendar(srv)
	if err != nil {
		log.Fatalf("Failed to select calendar: %v", err)
	}
	
	fmt.Println("\nChoose an operation:")
	fmt.Println("1. List upcoming events")
	fmt.Println("2. Create new event")
	fmt.Println("3. Get event details")
	fmt.Println("4. Update event")
	fmt.Println("5. Delete event")
	
	opChoice, err := promptForInput("\nEnter choice (1-5): ")
	if err != nil {
		log.Fatalf("Failed to read operation choice: %v", err)
	}
	
	var opErr error
	
	switch opChoice {
	case "1":
		opErr = listEvents(srv, calendarID)
	case "2":
		opErr = createEvent(srv, calendarID)
	case "3":
		opErr = getEvent(srv, calendarID)
	case "4":
		opErr = updateEvent(srv, calendarID)
	case "5":
		opErr = deleteEvent(srv, calendarID)
	default:
		log.Fatalf("Invalid operation: %s", opChoice)
	}
	
	if opErr != nil {
		log.Fatalf("Operation failed: %v", opErr)
	}
}