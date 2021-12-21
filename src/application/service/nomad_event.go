package service

import (
	"encoding/json"
	"log"
	"os"

	nomad "github.com/hashicorp/nomad/api"
	"github.com/jackc/pgx/v4"
	"github.com/pkg/errors"

	"github.com/input-output-hk/cicero/src/config"
	"github.com/input-output-hk/cicero/src/domain"
	"github.com/input-output-hk/cicero/src/domain/repository"
	"github.com/input-output-hk/cicero/src/infrastructure/persistence"
)

type NomadEventService interface {
	Save(pgx.Tx, *nomad.Event) error
	GetLastNomadEvent() (uint64, error)
	GetEventAllocByWorkflowId(uint64) (map[string]domain.AllocWrapper, error)
}

type nomadEventService struct {
	logger               *log.Logger
	nomadEventRepository repository.NomadEventRepository
	runService           RunService
}

func NewNomadEventService(db config.PgxIface, runService RunService) NomadEventService {
	return &nomadEventService{
		logger:               log.New(os.Stderr, "NomadEventService: ", log.LstdFlags),
		nomadEventRepository: persistence.NewNomadEventRepository(db),
		runService:           runService,
	}
}

func (n *nomadEventService) Save(tx pgx.Tx, event *nomad.Event) error {
	n.logger.Printf("Saving new NomadEvent %d", event.Index)
	if err := n.nomadEventRepository.Save(tx, event); err != nil {
		return errors.WithMessagef(err, "Could not insert NomadEvent")
	}
	n.logger.Printf("Created NomadEvent %d", event.Index)
	return nil
}

func (n *nomadEventService) GetLastNomadEvent() (uint64, error) {
	n.logger.Printf("Get last Nomad Event")
	return n.nomadEventRepository.GetLastNomadEvent()
}

func (n *nomadEventService) GetEventAllocByWorkflowId(workflowId uint64) (map[string]domain.AllocWrapper, error) {
	allocs := map[string]domain.AllocWrapper{}
	n.logger.Printf("Get EventAlloc by WorkflowId: %d", workflowId)
	results, err := n.nomadEventRepository.GetEventAllocByWorkflowId(workflowId)
	if err != nil {
		return nil, err
	}

	for _, result := range results {
		if result["alloc"] == nil {
			continue
		}

		alloc := &nomad.Allocation{}
		err = json.Unmarshal([]byte(result["alloc"].(string)), alloc)
		if err != nil {
			return nil, err
		}

		logs, err := n.runService.RunLogs(alloc.ID, alloc.TaskGroup)
		if err != nil {
			return nil, err
		}

		allocs[result["name"].(string)] = domain.AllocWrapper{Alloc: alloc, Logs: logs}
	}
	n.logger.Printf("Got EventAlloc by WorkflowId: %d", workflowId)
	return allocs, nil
}