package chatcompletionstream

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/LeandroMelloo/fcexperience-chatgpt/internal/domain/entity"
	"github.com/LeandroMelloo/fcexperience-chatgpt/internal/domain/gateway"
	openai "github.com/sashabaranov/go-openai"
)

type ChatCompletionConfigInputDTO struct {
	Model                string
	ModelMaxTokenx       int
	Temperature          float32
	TopP                 float32
	N                    int
	Stop                 []string
	MaxTokens            int
	PresencePenalty      float32
	FrequencyPenalty     float32
	InitialSystemMessage string
}

type ChatCompletionInputDTO struct {
	ChatID      string
	UserID      string
	UserMessage string
	Config      ChatCompletionConfigInputDTO
}

type ChatCompletionOutputDTO struct {
	ChatID  string
	UserID  string
	Content string
}

type ChatCompletionUseCase struct {
	ChatGateway  gateway.ChatGateway
	OpenAIClient *openai.Client
	Stream       chan ChatCompletionOutputDTO
}

func NewChatCompletionUseCase(chatGateway gateway.ChatGateway, openAiClient *openai.Client, streamchan ChatCompletionOutputDTO) *ChatCompletionUseCase {
	return &ChatCompletionUseCase{
		ChatGateway:  chatGateway,
		OpenAIClient: openAiClient,
	}
}

func (uc *ChatCompletionUseCase) Execute(ctx context.Context, input ChatCompletionInputDTO) (*ChatCompletionOutputDTO, error) {
	// Buscando os dados do usuário
	chat, err := uc.ChatGateway.FindChatByID(ctx, input.ChatID)
	// Se o chat não existir
	if err != nil {
		if err.Error() == "chat not found" {
			// create new chat(entity)
			chat, err = createNewChat(input)
			if err != nil {
				return nil, errors.New("error creating new chat: " + err.Error())
			}
			// Se não der erro salva o chat no banco de dados
			err = uc.ChatGateway.CreateChat(ctx, chat)
			if err != nil {
				return nil, errors.New("error persisting chat: " + err.Error())
			} else {
				return nil, errors.New("error fetching existing chat: " + err.Error())
			}
		}
	}
	// create user message
	userMessage, err := entity.NewMessage("user", input.UserMessage, chat.Config.Model)
	if err != nil {
		return nil, errors.New("error creating message: " + err.Error())
	}
	// adicionando a menssagem do usuário
	err = chat.AddMessage(userMessage)
	if err != nil {
		return nil, errors.New("error adding new message: " + err.Error())
	}
	// adicionando as mensagens na API da OpenAI
	messages := []openai.ChatCompletionMessage{}
	for _, msg := range chat.Messages {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}
	// chamando a API da OpenAI
	resp, err := uc.OpenAIClient.CreateChatCompletionStream(
		ctx,
		openai.ChatCompletionRequest{
			Model:            chat.Config.Model.Name,
			Messages:         messages,
			MaxTokens:        chat.Config.MaxTokens,
			Temperature:      chat.Config.Temperature,
			TopP:             chat.Config.TopP,
			PresencePenalty:  chat.Config.PresencePenalty,
			FrequencyPenalty: chat.Config.FrequencyPenalty,
			Stop:             chat.Config.Stop,
			Stream:           true,
		},
	)
	if err != nil {
		return nil, errors.New("error creating chat completion:" + err.Error())
	}

	var fullResponse strings.Builder

	// loop infinito
	for {
		response, err := resp.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errors.New("error streaming response: " + err.Error())
		}
		fullResponse.WriteString(response.Choices[0].Delta.Content)
		r := ChatCompletionOutputDTO{
			ChatID:  chat.ID,
			UserID:  input.UserID,
			Content: fullResponse.String(),
		}
		uc.Stream <- r
	}

	assistant, err := entity.NewMessage("assistant", fullResponse.String(), chat.Config.Model)
	if err != nil {
		return nil, errors.New("error creating assistant message: " + err.Error())
	}
	err = chat.AddMessage(assistant)
	if err != nil {
		return nil, errors.New("error adding new message: " + err.Error())
	}

	err = uc.ChatGateway.SaveChat(ctx, chat)
	if err != nil {
		return nil, errors.New("error saving chat: " + err.Error())
	}
	return &ChatCompletionOutputDTO{
		ChatID:  chat.ID,
		UserID:  input.UserID,
		Content: fullResponse.String(),
	}, nil
}

func createNewChat(input ChatCompletionInputDTO) (*entity.Chat, error) {
	model := entity.NewModel(input.Config.Model, input.Config.ModelMaxTokenx)
	chatConfig := &entity.ChatConfig{
		Temperature:      input.Config.Temperature,
		TopP:             input.Config.TopP,
		N:                input.Config.N,
		Stop:             input.Config.Stop,
		MaxTokens:        input.Config.MaxTokens,
		PresencePenalty:  input.Config.PresencePenalty,
		FrequencyPenalty: input.Config.FrequencyPenalty,
		Model:            model,
	}
	initialMessage, err := entity.NewMessage("system", input.Config.InitialSystemMessage, model)
	if err != nil {
		return nil, errors.New("error creating initial message: " + err.Error())
	}
	chat, err := entity.NewChat(input.UserID, initialMessage, chatConfig)
	if err != nil {
		return nil, errors.New("error creating new chat: " + err.Error())
	}
	return chat, nil
}
