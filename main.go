/*
Copyright © 2021 Amadeus_vn

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <http://www.gnu.org/licenses/>.
*/
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"

	"github.com/bwmarrin/discordgo"
	openai "github.com/sashabaranov/go-openai"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var (
	rootCmd = &cobra.Command{
		Use:   "amabot-chatgpt",
		Short: "amabot-chatgpt is a Discord Bot developed by @Amadeus_vn for personal use.",
	}
	// Path of the configuration file for this tool
	config_path = ""
)

const (
	MESSAGE_LENGTH = 5
)

func init() {
	// Flags of rootCmd
	rootCmd.PersistentFlags().StringVar(&config_path, "config-path", "", "Path of the configuration file for this tool")

	// Flags of Amabot
	rootCmd.PersistentFlags().StringP("token", "t", "", "Value of Discord API token")
	rootCmd.PersistentFlags().String("openai-token", "", "Value of OpenAI API token")
	rootCmd.PersistentFlags().StringSlice("openai-channels", []string{}, "ChannelID to listen")
	rootCmd.PersistentFlags().StringSlice("openai-systems", []string{}, "System message processed by ChatGPT")
	rootCmd.PersistentFlags().String("opts-sqlite", "amabot.sqlite3", "Sqlite3 Database-file to save data")

	// Read configuration file when it exists.
	cobra.OnInitialize(func() {
		if config_path != "" {
			_, err := os.Stat(config_path)
			cobra.CheckErr(err)
			viper.SetConfigFile(config_path)
		} else {
			home, err := os.UserHomeDir()
			cobra.CheckErr(err)
			viper.AddConfigPath(".")
			viper.AddConfigPath(home)
			viper.SetConfigType("yaml")
			viper.SetConfigName("amabot")
		}
		if err := viper.ReadInConfig(); err == nil {
			log.Println("Using config file:", viper.ConfigFileUsed())
		}

		viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
		viper.SetEnvPrefix("AMABOT")
		viper.AutomaticEnv()

		viper.BindPFlag("token", rootCmd.Flags().Lookup("token"))
		viper.BindPFlag("openai-token", rootCmd.Flags().Lookup("openai-token"))
		viper.BindPFlag("openai-systems", rootCmd.Flags().Lookup("openai-systems"))
		viper.BindPFlag("opts-sqlite", rootCmd.Flags().Lookup("opts-sqlite"))
	})
}

type Message struct {
	gorm.Model
	Content   string
	ChannelID string
	AuthorID  string
}

func main() {
	rootCmd.Run = func(cmd *cobra.Command, args []string) {
		// 各APIクライアントのセットアップ
		discord, err := discordgo.New("Bot " + viper.GetString("token"))
		openai_client := openai.NewClient(viper.GetString("openai-token"))

		// データベースのセットアップ
		db, err := gorm.Open(sqlite.Open(viper.GetString("opts-sqlite")), &gorm.Config{})
		if err != nil {
			panic("failed to connect database.")
		}
		db.AutoMigrate(&Message{})

		channels := viper.GetStringSlice("openai-channels")

		// 処理の登録
		discord.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
			// 登録したチャンネルの時のみ
			for _, ch := range channels {
				if m.ChannelID == ch {
					// メッセージの保存
					db.Create(&Message{
						Content:   m.Content,
						ChannelID: m.ChannelID,
						AuthorID:  m.Author.ID,
					})
					log.Println("Message created in DB.")

					// ボットの時などは処理しない
					if m.Author.ID == s.State.User.ID {
						return
					}
					if m.Author.Bot {
						return
					}

					var chatmsg []openai.ChatCompletionMessage
					for _, msg := range viper.GetStringSlice("openai-systems") {
						chatmsg = append(chatmsg, openai.ChatCompletionMessage{
							Role:    openai.ChatMessageRoleSystem,
							Content: msg,
						})
					}
					{
						var lastmsg []*Message
						db.Model(&Message{}).Where(&Message{ChannelID: m.ChannelID}).Order("created_at DESC").Limit(MESSAGE_LENGTH).Find(&lastmsg)
						for _, msg := range lastmsg {
							if msg.AuthorID == s.State.User.ID {
								chatmsg = append(chatmsg, openai.ChatCompletionMessage{
									Role:    openai.ChatMessageRoleAssistant,
									Content: msg.Content,
								})
							} else {
								chatmsg = append(chatmsg, openai.ChatCompletionMessage{
									Role:    openai.ChatMessageRoleUser,
									Content: msg.Content,
								})
							}
						}

						// おそうじ
						defer db.Model(&Message{}).Where(&Message{ChannelID: m.ChannelID}).Order("created_at DESC").Offset(len(lastmsg)).Delete(&Message{})
					}

					log.Println("Accessed OpenAI.")

					// ChatGPT呼び出し
					resp, err := openai_client.CreateChatCompletion(context.Background(), openai.ChatCompletionRequest{
						Model:    openai.GPT3Dot5Turbo,
						Messages: chatmsg,
					})
					if err != nil {
						log.Println("Error:", err)
						return
					}
					// 送信
					s.ChannelMessageSend(m.ChannelID, resp.Choices[0].Message.Content)
					log.Println("Message sent.")

					// ループ抜け出し
					return
				} else {
					go db.Model(&Message{}).Where(&Message{ChannelID: m.ChannelID}).Delete(nil)
				}
			}
		})

		if err := discord.Open(); err != nil {
			log.Fatal(err)
		}

		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt)
		log.Println("Press Ctrl+C to exit")
		<-stop
	}
	rootCmd.Execute()
}
