package mcp

import (
	"context"

	"github.com/maniack/catwatch/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
)

type Server struct {
	mcpServer *mcp.Server
	store     *storage.Store
}

func New(store *storage.Store) (*Server, error) {
	s := &Server{
		store: store,
	}

	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    "catwatch",
		Version: "1.0.0",
	}, nil)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "list_cats",
		Description: "Получить список всех котов с базовой информацией",
	}, s.listCats)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "get_cat",
		Description: "Получить подробную информацию о конкретном коте по его ID",
	}, s.getCat)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "search_cats",
		Description: "Поиск котов по имени",
	}, s.searchCats)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "get_cat_records",
		Description: "Получить историю кормления и медицинских процедур для кота",
	}, s.getCatRecords)

	s.mcpServer = mcpServer
	return s, nil
}

func (s *Server) Run(ctx context.Context) error {
	return s.mcpServer.Run(ctx, &mcp.StdioTransport{})
}

func (s *Server) HandleSSE() http.Handler {
	return mcp.NewSSEHandler(func(request *http.Request) *mcp.Server {
		return s.mcpServer
	}, nil)
}

type ListCatsArgs struct {
	Limit int `json:"limit"`
}

func (s *Server) listCats(ctx context.Context, request *mcp.CallToolRequest, input ListCatsArgs) (*mcp.CallToolResult, any, error) {
	var cats []storage.Cat
	query := s.store.DB.Preload("Tags").Order("created_at DESC")
	if input.Limit > 0 {
		query = query.Limit(input.Limit)
	}
	if err := query.Find(&cats).Error; err != nil {
		return nil, nil, err
	}
	return nil, cats, nil
}

type GetCatArgs struct {
	ID string `json:"id"`
}

func (s *Server) getCat(ctx context.Context, request *mcp.CallToolRequest, input GetCatArgs) (*mcp.CallToolResult, any, error) {
	var cat storage.Cat
	if err := s.store.DB.Preload("Locations").Preload("Images").Preload("Tags").Preload("Records").First(&cat, "id = ?", input.ID).Error; err != nil {
		return nil, nil, err
	}
	return nil, cat, nil
}

type SearchCatsArgs struct {
	Query string `json:"query"`
}

func (s *Server) searchCats(ctx context.Context, request *mcp.CallToolRequest, input SearchCatsArgs) (*mcp.CallToolResult, any, error) {
	var cats []storage.Cat
	if err := s.store.DB.Preload("Tags").Where("name LIKE ?", "%"+input.Query+"%").Find(&cats).Error; err != nil {
		return nil, nil, err
	}
	return nil, cats, nil
}

type GetCatRecordsArgs struct {
	CatID string `json:"cat_id"`
	Limit int    `json:"limit"`
}

func (s *Server) getCatRecords(ctx context.Context, request *mcp.CallToolRequest, input GetCatRecordsArgs) (*mcp.CallToolResult, any, error) {
	var records []storage.Record
	query := s.store.DB.Where("cat_id = ?", input.CatID).Order("timestamp DESC")
	if input.Limit > 0 {
		query = query.Limit(input.Limit)
	}
	if err := query.Find(&records).Error; err != nil {
		return nil, nil, err
	}
	return nil, records, nil
}
