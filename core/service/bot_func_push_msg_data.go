package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/AnnonaOrg/annona_bot/core/response"
	"github.com/AnnonaOrg/annona_bot/core/utils"
	log "github.com/sirupsen/logrus"
	tele "gopkg.in/telebot.v3"
)

var fifoMapMsgID *FIFOMap
var reciverFifoMap *FIFOMap

func init() {
	fifoMapMsgID = NewFIFOMap()
	reciverFifoMap = NewFIFOMap()
}

// 推送FeedMsg信息
func PushMsgData(data []byte) error {
	var msg response.FeedRichMsgResponse // FeedRichMsgModel
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Errorf("数据解析(%s)失败: %v", string(data), err)
		return err
	}
	if len(msg.MsgID) > 0 {
		msgID := "msgID_" + msg.MsgID

		if _, ok := fifoMapMsgID.Get(msgID); ok {
			return fmt.Errorf("msgID去重(%s)", msg.MsgID)
		} else {
			fifoMapMsgID.Set(msgID, true)
			if c := fifoMapMsgID.Count(); c > 50 {
				fifoMapMsgID.RemoveOldest()
			}
		}
	}

	return buildMsgDataAndSend(msg, SendMessage)
}

func buildMsgDataAndSend(msg response.FeedRichMsgResponse,
	sendMessage func(botToken string, reciverId int64, m interface{}, parseMode tele.ParseMode, noButton bool, button interface{}) error,
) error {
	reciverId := msg.ChatInfo.ToChatID
	botToken := msg.BotInfo.BotToken
	noButton := msg.NoButton
	reciverKey := fmt.Sprintf("%s_%d", msg.FormInfo.FormChatID, reciverId)
	if IsEnableFilterSameSenderUserMsg() {
		// reciverKey := fmt.Sprintf("%s_%d", msg.FormInfo.FormChatID, reciverId)
		if _, ok := reciverFifoMap.Get(reciverKey); ok {
			return fmt.Errorf("过滤短时间内同一个用户多次触发的消息(%s)", reciverKey)
		}
	}

	selector := &tele.ReplyMarkup{}
	selector2 := &tele.ReplyMarkup{}
	if !noButton {
		if len(msg.FormInfo.FormChatID) > 0 {
			noButton = false
		} else {
			noButton = true
		}

		btnSender := selector.Data("屏蔽号", "/block_formsenderid", msg.FormInfo.FormSenderID)
		btnChat := selector.Data("屏蔽群", "/block_formchatid", msg.FormInfo.FormChatID)
		btnByKeyworld := selector.Data("关键词", "/by_formkeyworld", msg.FormInfo.FormKeyworld)

		btnLink := selector.URL("定位消息", msg.Link)
		if len(msg.Link) == 0 {
			if len(msg.FormInfo.FormChatUsername) > 0 {
				btnLink = selector.URL("定位消息", "https://t.me/"+msg.FormInfo.FormChatUsername)
			}
		}
		btnByID := selector.Data("记录", "/by_formsenderid", msg.FormInfo.FormSenderID)
		btnChatLink := selector.URL("私聊", "tg://user?id="+msg.FormInfo.FormSenderID)
		if len(msg.FormInfo.FormSenderUsername) > 0 {
			btnChatLink = selector.URL("私聊", "https://t.me/"+msg.FormInfo.FormSenderUsername)
		} else if _, isINVALIDUserID := FIFOMapGet(msg.FormInfo.FormSenderID); isINVALIDUserID {
			// 已被标记 更换为IOS兼容私聊地址
			btnChatLink = selector.URL("私聊", "https://t.me/@id"+msg.FormInfo.FormSenderID)
		}

		selector2.Inline(
			selector2.Row(btnSender, btnChat, btnByKeyworld),
			selector2.Row(btnLink, btnByID),
		)
		selector.Inline(
			selector.Row(btnSender, btnChat, btnByKeyworld),
			selector.Row(btnLink, btnByID, btnChatLink),
		)

	}

	messageContentText := msg.Text.Content
	if len(messageContentText) > 0 {
		text := "关键词: #" + msg.FormInfo.FormKeyworld + " #ID" + msg.FormInfo.FormSenderID
		if len(msg.FormInfo.FormSenderTitle) > 0 {
			textTmp := "发送人: "
			if len(msg.FormInfo.FormSenderUsername) > 0 {
				textTmp = textTmp +
					fmt.Sprintf("<a href=\"https://t.me/%s\">%s</a>",
						msg.FormInfo.FormSenderUsername,
						utils.GetStringRuneN(msg.FormInfo.FormSenderTitle, 15),
					)
			} else {
				textTmp = textTmp + utils.GetStringRuneN(msg.FormInfo.FormSenderTitle, 15)
			}

			text = text + "\n" + textTmp
		}

		if len(msg.FormInfo.FormChatTitle) > 0 {
			textTmp := "来源: "
			if len(msg.FormInfo.FormChatUsername) > 0 {
				textTmp = textTmp +
					fmt.Sprintf("<a href=\"https://t.me/%s\">%s</a>",
						msg.FormInfo.FormChatUsername,
						utils.GetStringRuneN(msg.FormInfo.FormChatTitle, 15),
					)
			} else if len(msg.Link) > 0 {
				textTmp = textTmp +
					fmt.Sprintf("<a href=\"%s\">%s</a>",
						msg.Link,
						utils.GetStringRuneN(msg.FormInfo.FormChatTitle, 15),
					)
			} else {
				textTmp = textTmp + utils.GetStringRuneN(msg.FormInfo.FormChatTitle, 15)
			}

			text = text + "\n" + textTmp
		}

		messageContentText = text + "\n" + messageContentText
	} else {
		return fmt.Errorf("msg(%+v).Text.Content is NULL", msg)
	}
	// fmt.Println("messageContentText", messageContentText)
	log.Debugf("待发送消息:%s", messageContentText)

	switch msg.Msgtype {
	case "text":
		m := messageContentText
		if err := sendMessage(botToken, reciverId, m, tele.ModeHTML, noButton, selector); err != nil {
			log.Errorf("sendMessage(%d): %v", reciverId, err)
			return err
		}

	case "video":
		m := new(tele.Video)
		m.File = tele.FromURL(msg.Video.FileURL)
		if len(msg.Video.Caption) > 0 {
			m.Caption = msg.Video.Caption
		}
		if len(m.Caption) > 0 {
			if captionTmp, err := utils.UrlRegMatchReplaceToTGHTML(m.Caption); err != nil {
			} else {
				m.Caption = captionTmp
			}
		}
		if err := sendMessage(botToken, reciverId, m, tele.ModeHTML, noButton, selector); err != nil {
			log.Errorf("sendMessage(%d): %v", reciverId, err)
			return err
		}

	case "image":
		m := new(tele.Photo)
		m.File = tele.FromURL(msg.Image.PicURL)
		if len(msg.Image.Caption) > 0 {
			m.Caption = msg.Image.Caption
		}
		if len(m.Caption) > 0 {
			if captionTmp, err := utils.UrlRegMatchReplaceToTGHTML(m.Caption); err != nil {
			} else {
				m.Caption = captionTmp
			}
		}
		if err := sendMessage(botToken, reciverId, m, tele.ModeHTML, noButton, selector); err != nil {
			log.Errorf("sendMessage(%d): %v", reciverId, err)
			return err
		}

	case "rich":
		switch {
		case len(msg.Video.FileURL) > 0 && strings.HasPrefix(msg.Video.FileURL, "http"):
			{
				m := new(tele.Video)
				m.File = tele.FromURL(msg.Video.FileURL)
				if len(msg.Video.Caption) > 0 {
					m.Caption = msg.Video.Caption
				} else if len(msg.Text.Content) > 0 {
					m.Caption = msg.Text.Content
				}
				if len(m.Caption) > 0 {
					if captionTmp, err := utils.UrlRegMatchReplaceToTGHTML(m.Caption); err != nil {
					} else {
						m.Caption = captionTmp
					}
				}
				if err := sendMessage(botToken, reciverId, m, tele.ModeHTML, noButton, selector); err != nil {
					log.Errorf("sendMessage(%d): %v", reciverId, err)
					return err
				}
			}
		case len(msg.Image.PicURL) > 0 && strings.HasPrefix(msg.Image.PicURL, "http"):
			{
				m := new(tele.Photo)
				m.File = tele.FromURL(msg.Image.PicURL)
				if len(msg.Image.Caption) > 0 {
					m.Caption = msg.Image.Caption
				} else if len(msg.Text.Content) > 0 {
					m.Caption = msg.Text.Content
				}
				if len(m.Caption) > 0 {
					if captionTmp, err := utils.UrlRegMatchReplaceToTGHTML(m.Caption); err != nil {
					} else {
						m.Caption = captionTmp
					}
				}
				if err := sendMessage(botToken, reciverId, m, tele.ModeHTML, noButton, selector); err != nil {
					log.Errorf("sendMessage(%d): %v", reciverId, err)
					return err
				}
			}
		case len(msg.Text.Content) > 0:
			{
				m := messageContentText //msg.Text.Content

				err := sendMessage(botToken, reciverId, m, tele.ModeHTML, noButton, selector)
				if err != nil {
					if strings.Contains(err.Error(), "BUTTON_USER_INVALID") {
						if len(msg.FormInfo.FormSenderUsername) == 0 && len(msg.FormInfo.FormSenderID) > 0 {
							// 标记用户ID 不支持DeepLink私聊
							FIFOMapSet(msg.FormInfo.FormSenderID, "BUTTON_USER_INVALID")
							if count := FIFOMapCount(); count > 200 {
								FIFOMapRemoveOldest()
							}
						}
						if IsRetryPushMsgEnable() {
							if len(msg.FormInfo.FormSenderUsername) == 0 && (len(msg.Link) > 0 || len(msg.FormInfo.FormChatUsername) > 0) {
								if err := sendMessage(botToken, reciverId, m, tele.ModeHTML, noButton, selector2); err != nil {
									log.Errorf("sendMessage(%d): %v", reciverId, err)
									return err
								}
							}
						}

					}
					return err
				}

			}
		default:

		}
	default:
		return errors.New("msg type is not support,")
	}
	reciverFifoMap.Set(reciverKey, true)
	maxCount := GetMaxCountFilterSameSenderUserMsg()
	if c := reciverFifoMap.Count(); c > maxCount {
		reciverFifoMap.RemoveOldest()
	}
	return nil
}
