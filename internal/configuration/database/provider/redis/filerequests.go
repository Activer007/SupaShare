package redis

import (
	"cmp"
	"context"
	"slices"
	"strconv"

	"github.com/Activer007/SupaShare/internal/helper"
	"github.com/Activer007/SupaShare/internal/models"
	redigo "github.com/gomodule/redigo/redis"
)

const (
	prefixFileRequests = "frq:"
)

func dbToFileRequest(input []any) (models.FileRequest, error) {
	var result models.FileRequest
	err := redigo.ScanStruct(input, &result)
	if err != nil {
		return models.FileRequest{}, err
	}
	return result, nil
}

// GetFileRequest returns the FileRequest or false if not found
func (p DatabaseProvider) GetFileRequest(id string) (models.FileRequest, bool) {
	if id == "" {
		return models.FileRequest{}, false
	}
	result, ok := p.getHashMap(prefixFileRequests + id)
	if !ok {
		return models.FileRequest{}, false
	}
	request, err := dbToFileRequest(result)
	helper.Check(err)
	return request, true
}

// GetAllFileRequests returns an array with all file requests, ordered by creation date
func (p DatabaseProvider) GetAllFileRequests() []models.FileRequest {
	var result []models.FileRequest
	maps := p.getAllHashesWithPrefix(prefixFileRequests)
	for _, v := range maps {
		request, err := dbToFileRequest(v)
		helper.Check(err)
		result = append(result, request)
	}
	return sortFilerequests(result)
}

func sortFilerequests(users []models.FileRequest) []models.FileRequest {
	slices.SortFunc(users, func(a, b models.FileRequest) int {
		return cmp.Or(
			cmp.Compare(b.CreationDate, a.CreationDate),
			cmp.Compare(a.Name, b.Name),
		)
	})
	return users
}

// SaveFileRequest stores the file request associated with the file in the database
func (p DatabaseProvider) SaveFileRequest(request models.FileRequest) {
	helper.Check(p.writeFileRequestIndexed(context.Background(), request, false))
}

// DeleteFileRequest deletes a file request with the given ID
func (p DatabaseProvider) DeleteFileRequest(request models.FileRequest) {
	c := p.pool.Get()
	defer c.Close()
	current, ok := p.GetFileRequest(request.Id)
	if !ok {
		return
	}
	_, err := c.Do("EVAL", `redis.call('DEL',KEYS[1]);redis.call('ZREM',KEYS[2],ARGV[1]);redis.call('ZREM',KEYS[3],ARGV[1]);redis.call('ZREM',KEYS[4],ARGV[2]);return 1`, 4, p.dbPrefix+prefixFileRequests+request.Id, p.dbPrefix+frIndex+"all", p.dbPrefix+frIndex+"owner:"+strconv.Itoa(current.UserId), p.dbPrefix+frIndex+"expiry", models.FileRequestCursor(current), current.Id)
	helper.Check(err)
}
