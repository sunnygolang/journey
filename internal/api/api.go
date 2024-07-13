package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sunnygolang/journey/internal/api/spec"
	"github.com/sunnygolang/journey/internal/pgstore"
	"go.uber.org/zap"
)

type store interface {
	GetTrip(ctx context.Context, id uuid.UUID) (pgstore.Trip, error)
	UpdateTrip(ctx context.Context, arg pgstore.UpdateTripParams) error
	GetTripActivities(ctx context.Context, tripID uuid.UUID) ([]pgstore.Activity, error)
	GetParticipant(ctx context.Context, participantID uuid.UUID) (pgstore.Participant, error)
	ConfirmParticipant(ctx context.Context, participantID uuid.UUID) error
	CreateTrip(ctx context.Context, pool *pgxpool.Pool, params spec.CreateTripRequest) (uuid.UUID, error)
}

type mailer interface {
	SendConfirmTripEmailToTripOwner(tripID uuid.UUID) error
}

type API struct {
	store     store
	logger    *zap.Logger
	validator *validator.Validate
	pool      *pgxpool.Pool
	mailer    mailer
}

func NewAPI(pool *pgxpool.Pool, logger *zap.Logger, mailer mailer) API {
	validator := validator.New(validator.WithRequiredStructEnabled())
	return API{pgstore.New(pool), logger, validator, pool, mailer}
}

// Confirms a participant on a trip.
// (PATCH /participants/{participantId}/confirm)
func (api API) PatchParticipantsParticipantIDConfirm(w http.ResponseWriter, r *http.Request, participantID string) *spec.Response {
	id, err := uuid.Parse(participantID)
	if err != nil {
		return spec.PatchParticipantsParticipantIDConfirmJSON400Response(spec.Error{Message: "invalid uuid"})
	}

	participant, err := api.store.GetParticipant(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return spec.PatchParticipantsParticipantIDConfirmJSON400Response(spec.Error{Message: "participant not found"})
		}
		api.logger.Error("failed to get participant", zap.Error(err), zap.String("participant_id", participantID))
		return spec.PatchParticipantsParticipantIDConfirmJSON400Response(spec.Error{Message: "something went wrong, try again"})
	}

	if participant.IsConfirmed {
		return spec.PatchParticipantsParticipantIDConfirmJSON400Response(spec.Error{Message: "participant already confirmed"})
	}

	if err := api.store.ConfirmParticipant(r.Context(), id); err != nil {
		api.logger.Error("failed to confirm participant", zap.Error(err), zap.String("participant_id", participantID))
		return spec.PatchParticipantsParticipantIDConfirmJSON400Response(spec.Error{Message: "something went wrong, try again"})
	}

	return spec.PatchParticipantsParticipantIDConfirmJSON204Response(nil)
}

// Create a new trip
// (POST /trips)
func (api API) PostTrips(w http.ResponseWriter, r *http.Request) *spec.Response {
	var body spec.CreateTripRequest

	err := json.NewDecoder(r.Body).Decode(&body)
	if err != nil {
		spec.PostTripsJSON400Response(spec.Error{Message: "invalid json: " + err.Error()})
	}

	if err := api.validator.Struct(body); err != nil {
		return spec.PostTripsJSON400Response(spec.Error{Message: "invalid input: " + err.Error()})
	}

	tripID, err := api.store.CreateTrip(r.Context(), api.pool, body)
	if err != nil {
		return spec.PostTripsJSON400Response(spec.Error{Message: "failed to create trip, try again"})
	}

	go func() {
		if err := api.mailer.SendConfirmTripEmailToTripOwner(tripID); err != nil {
			api.logger.Error(
				"failed to send email on PostTrips",
				zap.Error(err),
				zap.String("trip_id", tripID.String()),
			)
		}
	}()

	return spec.PostTripsJSON201Response(spec.CreateTripResponse{TripID: tripID.String()})
}

// Get a trip details.
// (GET /trips/{tripId})
func (api API) GetTripsTripID(w http.ResponseWriter, r *http.Request, tripID string) *spec.Response {
	id, err := uuid.Parse(tripID)
	if err != nil {
		return spec.GetTripsTripIDJSON400Response(spec.Error{Message: "invalid uuid passed: " + err.Error()})
	}

	trip, err := api.store.GetTrip(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return spec.GetTripsTripIDJSON400Response(spec.Error{Message: "trip not found"})
		}
		api.logger.Error("failed to get trip by id", zap.Error(err), zap.String("trip_id", tripID))
		return spec.GetTripsTripIDJSON400Response(spec.Error{Message: "something went wrong, try again"})
	}

	return spec.GetTripsTripIDJSON200Response(spec.GetTripDetailsResponse{
		Trip: spec.GetTripDetailsResponseTripObj{
			Destination: trip.Destination,
			EndsAt:      trip.EndsAt.Time,
			ID:          trip.ID.String(),
			IsConfirmed: trip.IsConfirmed,
			StartsAt:    trip.StartsAt.Time,
		},
	})
}

// Update a trip.
// (PUT /trips/{tripId})
func (api API) PutTripsTripID(w http.ResponseWriter, r *http.Request, tripID string) *spec.Response {
	var body spec.PutTripsTripIDJSONRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return spec.PutTripsTripIDJSON400Response(spec.Error{Message: err.Error()})
	}

	if len(body.Destination) < 4 {
		return spec.PutTripsTripIDJSON400Response(struct {
			Message string `json:"message"`
		}{Message: "destination must be ate least 4 characters long"})
	}

	id, err := uuid.Parse(tripID)
	if err != nil {
		return spec.PostTripsJSON400Response(spec.Error{Message: "invalid uuid passed: " + err.Error()})
	}

	trip, err := api.store.GetTrip(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return spec.PostTripsJSON400Response(spec.Error{Message: "trip not found"})
		}
		api.logger.Error("failed to get trip by id", zap.Error(err), zap.String("trip_id", tripID))
		return spec.PutTripsTripIDJSON400Response(spec.Error{Message: "something went wrong, try again"})
	}

	if err := api.store.UpdateTrip(r.Context(), pgstore.UpdateTripParams{
		Destination: body.Destination,
		EndsAt:      pgtype.Timestamp{Time: body.EndsAt, Valid: true},
		StartsAt:    pgtype.Timestamp{Time: body.StartsAt, Valid: true},
		ID:          id,
		IsConfirmed: trip.IsConfirmed,
	}); err != nil {
		api.logger.Error("failed to update trip", zap.Error(err), zap.String("trip_id", tripID))
		return spec.PutTripsTripIDJSON400Response(spec.Error{Message: "something went wrong, try again"})
	}

	return spec.PutTripsTripIDJSON204Response(nil)
}

// Get a trip activities.
// (GET /trips/{tripId}/activities)
func (api API) GetTripsTripIDActivities(w http.ResponseWriter, r *http.Request, tripID string) *spec.Response {
	id, err := uuid.Parse(tripID)
	if err != nil {
		return spec.GetTripsTripIDActivitiesJSON400Response(spec.Error{Message: "invalid uuid passed: " + err.Error()})
	}

	activities, err := api.store.GetTripActivities(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return spec.GetTripsTripIDActivitiesJSON400Response(spec.Error{Message: "trip not found"})
		}
		api.logger.Error("failed to find trip participants", zap.Error(err), zap.String("trip_id", tripID))
		return spec.GetTripsTripIDActivitiesJSON400Response(spec.Error{Message: "something went wrong, try again"})
	}

	var output spec.GetTripActivitiesResponse

	groupedActivites := make(map[string][]pgstore.Activity)

	for _, act := range activities {
		date := act.OccursAt.Time.Format(time.DateOnly)
		groupedActivites[date] = append(groupedActivites[date], act)
	}

	for dateStr, actsOnDate := range groupedActivites {
		var innerActs []spec.GetTripActivitiesResponseInnerArray

		for _, act := range actsOnDate {
			innerActs = append(innerActs, spec.GetTripActivitiesResponseInnerArray{
				ID:       act.ID.String(),
				OccursAt: act.OccursAt.Time,
				Title:    act.Title,
			})
		}

		date, _ := time.Parse(time.DateOnly, dateStr)
		output.Activities = append(output.Activities, spec.GetTripActivitiesResponseOuterArray{
			Date:       date,
			Activities: innerActs,
		})
	}

	return spec.GetTripsTripIDActivitiesJSON200Response(output)
}

// Create a trip activity.
// (POST /trips/{tripId}/activities)
func (api API) PostTripsTripIDActivities(w http.ResponseWriter, r *http.Request, tripID string) *spec.Response {
	panic("not implemented") // TODO: Implement
}

// Confirm a trip and send e-mail invitations.
// (GET /trips/{tripId}/confirm)
func (api API) GetTripsTripIDConfirm(w http.ResponseWriter, r *http.Request, tripID string) *spec.Response {
	panic("not implemented") // TODO: Implement
}

// Invite someone to the trip.
// (POST /trips/{tripId}/invites)
func (api API) PostTripsTripIDInvites(w http.ResponseWriter, r *http.Request, tripID string) *spec.Response {
	panic("not implemented") // TODO: Implement
}

// Get a trip links.
// (GET /trips/{tripId}/links)
func (api API) GetTripsTripIDLinks(w http.ResponseWriter, r *http.Request, tripID string) *spec.Response {
	panic("not implemented") // TODO: Implement
}

// Create a trip link.
// (POST /trips/{tripId}/links)
func (api API) PostTripsTripIDLinks(w http.ResponseWriter, r *http.Request, tripID string) *spec.Response {
	panic("not implemented") // TODO: Implement
}

// Get a trip participants.
// (GET /trips/{tripId}/participants)
func (api API) GetTripsTripIDParticipants(w http.ResponseWriter, r *http.Request, tripID string) *spec.Response {
	panic("not implemented") // TODO: Implement
}
