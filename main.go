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
	"reflect"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	openai "github.com/sashabaranov/go-openai"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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
	MESSAGE_LENGTH  = 6
	MESSAGE_TIMEOUT = time.Hour * 24
)

func init() {
	// Flags of rootCmd
	rootCmd.PersistentFlags().StringVar(&config_path, "config-path", "", "Path of the configuration file for this tool")

	// Flags of Amabot
	rootCmd.PersistentFlags().StringP("token", "t", "", "Value of Discord API token")
	rootCmd.PersistentFlags().String("openai-token", "", "Value of OpenAI API token")
	rootCmd.PersistentFlags().StringSlice("openai-channels", []string{}, "ChannelID to listen")
	rootCmd.PersistentFlags().StringSlice("openai-systems", []string{}, "System message processed by ChatGPT")

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
	})
}

// 配列を逆順にする。
func reverse(s interface{}) {
	n := reflect.ValueOf(s).Len()
	swap := reflect.Swapper(s)
	for i, j := 0, n-1; i < j; i, j = i+1, j-1 {
		swap(i, j)
	}
}

func main() {
	rootCmd.Run = func(cmd *cobra.Command, args []string) {
		// 各APIクライアントのセットアップ
		discord, err := discordgo.New("Bot " + viper.GetString("token"))
		if err != nil {
			panic("failed to setup discord instance.")
		}
		openai_client := openai.NewClient(viper.GetString("openai-token"))

		channels := viper.GetStringSlice("openai-channels")

		// 処理の登録
		discord.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
			// 登録したチャンネルの時のみ
			for _, ch := range channels {
				if m.ChannelID == ch {
					// ボットの時などは処理しない
					if m.Author.ID == s.State.User.ID {
						return
					}
					if m.Author.Bot {
						return
					}

					// タイピング
					s.ChannelTyping(m.ChannelID)

					var chatmsg []openai.ChatCompletionMessage
					for _, msg := range viper.GetStringSlice("openai-systems") {
						chatmsg = append(chatmsg, openai.ChatCompletionMessage{
							Role:    openai.ChatMessageRoleSystem,
							Content: msg,
						})
					}
					{
						lastmsg, err := discord.ChannelMessages(m.ChannelID, MESSAGE_LENGTH, m.ID, "", "")
						if err != nil {
							log.Println("Error:", err)
							return
						}
						reverse(lastmsg)
						lastmsg = append(lastmsg, m.Message)

						for _, msg := range lastmsg {
							if msg.Timestamp.Add(MESSAGE_TIMEOUT).After(msg.Timestamp) {
								if msg.Author.ID == s.State.User.ID {
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
						}
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
