package requests

import (
	"encoding/json"
	"fmt"
	"image/color"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"gioui.org/font"
	"gioui.org/io/clipboard"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/dustin/go-humanize"
	"github.com/mirzakhany/chapar/internal/notify"
	"github.com/mirzakhany/chapar/internal/rest"
	"github.com/mirzakhany/chapar/ui/widgets"
)

type RestContainer struct {
	// Request Bar
	methodDropDown *widgets.DropDown
	addressMutex   *sync.Mutex

	updateAddress bool
	address       *widget.Editor
	sendClickable widget.Clickable
	sendButton    material.ButtonStyle

	// Response
	responseHeadersList *widget.List
	responseCookiesList *widget.List
	responseHeaders     []keyValue
	responseCookies     []keyValue
	loading             bool
	resultUpdated       bool
	result              string

	jsonViewer *widgets.JsonViewer

	// copyClickable *widget.Clickable
	// saveClickable      *widget.Clickable
	copyResponseButton *widgets.FlatButton
	// saveResponseButton *widgets.FlatButton
	responseTabs *widgets.Tabs

	// Request
	requestBody         *widgets.CodeEditor
	requestBodyDropDown *widgets.DropDown
	requestBodyBinary   *widgets.TextField
	resultStatus        string
	requestTabs         *widgets.Tabs
	preRequestDropDown  *widgets.DropDown
	preRequestBody      *widgets.CodeEditor
	postRequestDropDown *widgets.DropDown
	postRequestBody     *widgets.CodeEditor

	queryParams       *widgets.KeyValue
	updateQueryParams bool
	formDataParams    *widgets.KeyValue
	urlEncodedParams  *widgets.KeyValue
	pathParams        *widgets.KeyValue
	headers           *widgets.KeyValue

	split widgets.SplitView
}

type keyValue struct {
	Key   string
	Value string

	keySelectable   *widget.Selectable
	valueSelectable *widget.Selectable
}

func NewRestContainer(theme *material.Theme) *RestContainer {
	r := &RestContainer{
		split: widgets.SplitView{
			Ratio:         0,
			BarWidth:      unit.Dp(2),
			BarColor:      color.NRGBA{R: 0x2b, G: 0x2d, B: 0x31, A: 0xff},
			BarColorHover: theme.Palette.ContrastBg,
		},
		address:           new(widget.Editor),
		requestBody:       widgets.NewCodeEditor(""),
		preRequestBody:    widgets.NewCodeEditor(""),
		postRequestBody:   widgets.NewCodeEditor(""),
		requestBodyBinary: widgets.NewTextField("", "Select file"),
		responseHeadersList: &widget.List{
			List: layout.List{
				Axis: layout.Vertical,
			},
		},

		responseCookiesList: &widget.List{
			List: layout.List{
				Axis: layout.Vertical,
			},
		},
		jsonViewer: widgets.NewJsonViewer(),

		queryParams: widgets.NewKeyValue(
			widgets.NewKeyValueItem("", "", "", false),
		),

		pathParams: widgets.NewKeyValue(
			widgets.NewKeyValueItem("", "", "", false),
		),

		headers: widgets.NewKeyValue(
			widgets.NewKeyValueItem("", "", "", false),
		),

		formDataParams: widgets.NewKeyValue(
			widgets.NewKeyValueItem("", "", "", false),
		),
		urlEncodedParams: widgets.NewKeyValue(
			widgets.NewKeyValueItem("", "", "", false),
		),

		addressMutex: &sync.Mutex{},
	}

	r.copyResponseButton = &widgets.FlatButton{
		Text:            "Copy",
		BackgroundColor: theme.Palette.Bg,
		TextColor:       theme.Palette.Fg,
		MinWidth:        unit.Dp(75),
		Icon:            widgets.CopyIcon,
		IconPosition:    widgets.FlatButtonIconEnd,
		SpaceBetween:    unit.Dp(5),
	}

	r.requestBodyBinary.SetIcon(widgets.UploadIcon, widgets.IconPositionEnd)

	search := widgets.NewTextField("", "Search...")
	search.SetIcon(widgets.SearchIcon, widgets.IconPositionEnd)

	r.queryParams.SetOnChanged(r.onQueryParamChange)

	r.sendButton = material.Button(theme, &r.sendClickable, "Send")
	r.requestTabs = widgets.NewTabs([]*widgets.Tab{
		{Title: "Params"},
		{Title: "Body"},
		{Title: "Headers"},
		{Title: "Pre-req"},
		{Title: "Post-req"},
	}, nil)

	r.responseTabs = widgets.NewTabs([]*widgets.Tab{
		{Title: "Body"},
		{Title: "Headers"},
		{Title: "Cookies"},
	}, nil)

	r.methodDropDown = widgets.NewDropDownWithoutBorder(
		widgets.NewDropDownOption("GET"),
		widgets.NewDropDownOption("POST"),
		widgets.NewDropDownOption("PUT"),
		widgets.NewDropDownOption("PATCH"),
		widgets.NewDropDownOption("DELETE"),
		widgets.NewDropDownOption("HEAD"),
		widgets.NewDropDownOption("OPTION"),
	)

	r.preRequestDropDown = widgets.NewDropDown(
		widgets.NewDropDownOption("None"),
		widgets.NewDropDownOption("Python Script"),
		widgets.NewDropDownOption("SSH Script"),
		widgets.NewDropDownOption("SSH Tunnel"),
		widgets.NewDropDownOption("Kubectl Tunnel"),
	)

	r.postRequestDropDown = widgets.NewDropDown(
		widgets.NewDropDownOption("None"),
		widgets.NewDropDownOption("Python Script"),
		widgets.NewDropDownOption("SSH Script"),
	)

	r.requestBodyDropDown = widgets.NewDropDown(
		widgets.NewDropDownOption("None"),
		widgets.NewDropDownOption("JSON"),
		widgets.NewDropDownOption("Text"),
		widgets.NewDropDownOption("XML"),
		widgets.NewDropDownOption("Form data"),
		widgets.NewDropDownOption("Binary"),
		widgets.NewDropDownOption("Urlencoded"),
	)
	r.address.SingleLine = true
	r.address.SetText("https://jsonplaceholder.typicode.com/comments")

	return r
}

func (r *RestContainer) Submit() {
	method := r.methodDropDown.GetSelected().Text
	address := r.address.Text()
	headers := make(map[string]string)
	for _, h := range r.headers.GetItems() {
		if h.Key == "" || !h.Active || h.Value == "" {
			continue
		}
		headers[h.Key] = h.Value
	}

	body, contentType := r.prepareBody()
	headers["Content-Type"] = contentType

	r.resultStatus = ""
	r.sendButton.Text = "Cancel"
	r.loading = true
	r.resultUpdated = false
	defer func() {
		r.sendButton.Text = "Send"
		r.loading = false
		r.resultUpdated = false
	}()

	res, err := rest.DoRequest(&rest.Request{
		URL:     address,
		Method:  method,
		Headers: headers,
		Body:    body,
	})
	if err != nil {
		r.result = err.Error()
		return
	}

	dataStr := string(res.Body)
	if rest.IsJSON(dataStr) {
		var data map[string]interface{}
		if err := json.Unmarshal(res.Body, &data); err != nil {
			r.result = err.Error()
			return
		}
		var err error
		dataStr, err = rest.PrettyJSON(res.Body)
		if err != nil {
			r.result = err.Error()
			return
		}
	}

	// format response status
	r.resultStatus = fmt.Sprintf("%d %s, %s, %s", res.StatusCode, http.StatusText(res.StatusCode), res.TimePassed, humanize.Bytes(uint64(len(res.Body))))
	r.responseHeaders = make([]keyValue, 0)
	for k, v := range res.Headers {
		r.responseHeaders = append(r.responseHeaders, keyValue{
			Key:             k,
			Value:           v,
			keySelectable:   &widget.Selectable{},
			valueSelectable: &widget.Selectable{},
		})
	}

	// response cookies
	r.responseCookies = make([]keyValue, 0)
	for _, c := range res.Cookies {
		r.responseCookies = append(r.responseCookies, keyValue{
			Key:             c.Name,
			Value:           c.Value,
			keySelectable:   &widget.Selectable{},
			valueSelectable: &widget.Selectable{},
		})
	}

	r.result = dataStr
}

func (r *RestContainer) prepareBody() ([]byte, string) {
	switch r.requestBodyDropDown.SelectedIndex() {
	case 0: // none
		return nil, ""
	case 1: // json
		return []byte(r.requestBody.Code()), "application/json"
	case 2, 3: // text, xml
		return []byte(r.requestBody.Code()), "application/text"
	case 4: // form data
		return nil, "application/form-data"
	case 5: // binary
		return nil, "application/octet-stream"
	case 6: // urlencoded
		return nil, "application/x-www-form-urlencoded"
	}

	return nil, ""
}

func (r *RestContainer) copyResponseToClipboard(gtx layout.Context) {
	switch r.responseTabs.Selected() {
	case 0:
		if r.result == "" {
			return
		}

		gtx.Execute(clipboard.WriteCmd{
			Data: io.NopCloser(strings.NewReader(r.result)),
		})
		notify.Send("Response copied to clipboard", time.Second*3)
	case 1:
		if len(r.responseHeaders) == 0 {
			return
		}

		headers := ""
		for _, h := range r.responseHeaders {
			headers += fmt.Sprintf("%s: %s\n", h.Key, h.Value)
		}

		gtx.Execute(clipboard.WriteCmd{
			Data: io.NopCloser(strings.NewReader(headers)),
		})
		notify.Send("Response headers copied to clipboard", time.Second*3)
	case 2:
		if len(r.responseCookies) == 0 {
			return
		}

		cookies := ""
		for _, c := range r.responseCookies {
			cookies += fmt.Sprintf("%s: %s\n", c.Key, c.Value)
		}

		gtx.Execute(clipboard.WriteCmd{
			Data: io.NopCloser(strings.NewReader(cookies)),
		})
		notify.Send("Response cookies copied to clipboard", time.Second*3)
	}
}

func (r *RestContainer) onQueryParamChange(items []*widgets.KeyValueItem) {
	if r.updateQueryParams {
		r.updateQueryParams = false
		return
	}

	addr := r.address.Text()
	if addr == "" {
		return
	}

	// Parse the existing URL
	parsedURL, err := url.Parse(addr)
	if err != nil {
		fmt.Println("Error parsing URL:", err)
		return
	}

	// Parse the query parameters from the URL
	queryParams := parsedURL.Query()

	// Iterate over the items and update the query parameters
	for _, item := range items {
		if item.Active && item.Key != "" && item.Value != "" {
			// Set the parameter only if both key and value are non-empty
			queryParams.Set(item.Key, item.Value)
		} else {
			// Remove the parameter if the item is not active or key/value is empty
			queryParams.Del(item.Key)
		}
	}

	// delete items that are not exit in items
	for k := range queryParams {
		found := false
		for _, item := range items {
			if item.Active && item.Key == k {
				found = true
				break
			}
		}
		if !found {
			queryParams.Del(k)
		}
	}

	parsedURL.RawQuery = queryParams.Encode()
	finalURL := parsedURL.String()
	r.addressMutex.Lock()
	r.updateAddress = true

	_, coll := r.address.CaretPos()
	r.address.SetText(finalURL)
	r.address.SetCaret(coll, coll+1)

	r.addressMutex.Unlock()
}

func (r *RestContainer) addressChanged() {
	// Parse the existing URL
	parsedURL, err := url.Parse(r.address.Text())
	if err != nil {
		fmt.Println("Error parsing URL:", err)
		return
	}

	// Parse the query parameters from the URL
	queryParams := parsedURL.Query()

	items := make([]*widgets.KeyValueItem, 0)
	for k, v := range queryParams {
		if len(v) > 0 {
			// Add the parameter as a new key-value item
			items = append(items, widgets.NewKeyValueItem(k, v[0], "", true))
		}
	}

	r.updateQueryParams = true
	r.queryParams.SetItems(items)
}

func (r *RestContainer) requestBar(gtx layout.Context, theme *material.Theme) layout.Dimensions {
	border := widget.Border{
		Color:        widgets.Gray400,
		Width:        unit.Dp(1),
		CornerRadius: unit.Dp(4),
	}

	for {
		event, ok := r.address.Update(gtx)
		if !ok {
			break
		}
		if _, ok := event.(widget.ChangeEvent); ok {
			if !r.updateAddress {
				r.addressChanged()
			} else {
				r.updateAddress = false
			}
		}
	}

	return border.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{
			Axis:      layout.Horizontal,
			Alignment: layout.Middle,
			Spacing:   layout.SpaceEnd,
		}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return r.methodDropDown.Layout(gtx, theme)
			}),
			widgets.VerticalLine(40.0),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(10), Right: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					r.addressMutex.Lock()
					defer r.addressMutex.Unlock()
					return material.Editor(theme, r.address, "https://example.com").Layout(gtx)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					if r.sendClickable.Clicked(gtx) {
						go r.Submit()
					}

					gtx.Constraints.Min.X = gtx.Dp(80)
					return r.sendButton.Layout(gtx)
				})
			}),
		)
	})
}

func (r *RestContainer) requestBodyLayout(gtx layout.Context, theme *material.Theme) layout.Dimensions {
	return layout.Flex{
		Axis:      layout.Vertical,
		Alignment: layout.Start,
	}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return material.Label(theme, theme.TextSize, "Request body").Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return r.requestBodyDropDown.Layout(gtx, theme)
				}),
			)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(5)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				switch r.requestBodyDropDown.SelectedIndex() {
				case 1, 2, 3: // json, text, xml
					hint := ""
					if r.requestBodyDropDown.SelectedIndex() == 1 {
						hint = "Enter json"
					} else if r.requestBodyDropDown.SelectedIndex() == 2 {
						hint = "Enter text"
					} else if r.requestBodyDropDown.SelectedIndex() == 3 {
						hint = "Enter xml"
					}

					return r.requestBody.Layout(gtx, theme, hint)
				case 4: // form data
					return layout.Flex{
						Axis:      layout.Vertical,
						Alignment: layout.Start,
					}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return r.formDataParams.WithAddLayout(gtx, "", "", theme)
						}),
					)
				case 5: // binary
					return r.requestBodyBinary.Layout(gtx, theme)
				case 6: // urlencoded
					return layout.Flex{
						Axis:      layout.Vertical,
						Alignment: layout.Start,
					}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return r.urlEncodedParams.WithAddLayout(gtx, "", "", theme)
						}),
					)
				default:
					return layout.Dimensions{}
				}
			})
		}),
	)
}

func (r *RestContainer) requestPostReqLayout(gtx layout.Context, theme *material.Theme) layout.Dimensions {
	return layout.Flex{
		Axis:      layout.Vertical,
		Alignment: layout.Start,
	}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return material.Label(theme, theme.TextSize, "Action to do after request").Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return r.postRequestDropDown.Layout(gtx, theme)
				}),
			)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			hint := ""
			if r.postRequestDropDown.SelectedIndex() == 1 {
				hint = "Python script"
			} else if r.postRequestDropDown.SelectedIndex() == 2 {
				hint = "SSH script"
			} else {
				return layout.Dimensions{}
			}

			return layout.UniformInset(unit.Dp(5)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return r.postRequestBody.Layout(gtx, theme, hint)
			})
		}),
	)
}

func (r *RestContainer) requestPreReqLayout(gtx layout.Context, theme *material.Theme) layout.Dimensions {
	return layout.Flex{
		Axis:      layout.Vertical,
		Alignment: layout.Start,
	}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return material.Label(theme, theme.TextSize, "Action to do before request").Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return r.preRequestDropDown.Layout(gtx, theme)
				}),
			)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			hint := ""
			if r.preRequestDropDown.SelectedIndex() == 1 {
				hint = "Python script"
			} else if r.preRequestDropDown.SelectedIndex() == 2 {
				hint = "SSH script"
			} else {
				return layout.Dimensions{}
			}

			return layout.UniformInset(unit.Dp(5)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return r.preRequestBody.Layout(gtx, theme, hint)
			})
		}),
	)
}

func (r *RestContainer) paramsLayout(gtx layout.Context, theme *material.Theme) layout.Dimensions {
	return layout.Flex{
		Axis:      layout.Vertical,
		Alignment: layout.Start,
	}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return r.queryParams.WithAddLayout(gtx, "Query", "", theme)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(15)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return r.pathParams.WithAddLayout(gtx, "Path", "path params inside bracket, for example: {id}", theme)
		}),
	)
}

func (r *RestContainer) requestBodyFormDataLayout(gtx layout.Context, theme *material.Theme) layout.Dimensions {
	return layout.Flex{
		Axis:      layout.Vertical,
		Alignment: layout.Start,
	}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return r.queryParams.WithAddLayout(gtx, "Query", "", theme)
		}),
	)
}

func (r *RestContainer) requestLayout(gtx layout.Context, theme *material.Theme) layout.Dimensions {
	return layout.Flex{
		Axis:      layout.Vertical,
		Alignment: layout.Start,
	}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return r.requestTabs.Layout(gtx, theme)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(5)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				switch r.requestTabs.Selected() {
				case 0:
					return r.paramsLayout(gtx, theme)
				case 1:
					return r.requestBodyLayout(gtx, theme)
				case 2:
					return r.headers.WithAddLayout(gtx, "Headers", "", theme)
				case 3:
					return r.requestPreReqLayout(gtx, theme)
				case 4:
					return r.requestPostReqLayout(gtx, theme)
				}
				return layout.Dimensions{}
			})
		}),
	)
}

func (r *RestContainer) responseKeyValue(gtx layout.Context, theme *material.Theme, state *widget.List, itemType string, items []keyValue) layout.Dimensions {
	if len(items) == 0 {
		return r.messageLayout(gtx, theme, fmt.Sprintf("No %s available", itemType))
	}

	return material.List(theme, state).Layout(gtx, len(items), func(gtx layout.Context, i int) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.UniformInset(unit.Dp(5)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					l := material.Label(theme, theme.TextSize, items[i].Key+":")
					l.Font.Weight = font.Bold
					l.State = items[i].keySelectable
					return l.Layout(gtx)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					l := material.Label(theme, theme.TextSize, items[i].Value)
					l.State = items[i].valueSelectable
					return l.Layout(gtx)
				})
			}),
		)
	})
}

func (r *RestContainer) messageLayout(gtx layout.Context, theme *material.Theme, message string) layout.Dimensions {
	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		l := material.LabelStyle{
			Text:     message,
			Color:    widgets.Gray600,
			TextSize: theme.TextSize,
			Shaper:   theme.Shaper,
		}
		l.Font.Typeface = theme.Face
		return l.Layout(gtx)
	})
}

func (r *RestContainer) responseLayout(gtx layout.Context, theme *material.Theme) layout.Dimensions {
	if r.result == "" {
		return r.messageLayout(gtx, theme, "No response available yet ;)")
	}

	if r.copyResponseButton.Clickable.Clicked(gtx) {
		r.copyResponseToClipboard(gtx)
	}

	return layout.Flex{
		Axis: layout.Vertical,
	}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return r.responseTabs.Layout(gtx, theme)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceBetween, Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.LabelStyle{
							Text:     r.resultStatus,
							Color:    widgets.LightGreen,
							TextSize: theme.TextSize,
							Shaper:   theme.Shaper,
						}
						l.Font.Typeface = theme.Face
						return l.Layout(gtx)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return r.copyResponseButton.Layout(gtx, theme)
				}),
				//layout.Rigid(layout.Spacer{Width: unit.Dp(2)}.Layout),
				//layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				//	return r.saveResponseButton.Layout(gtx, theme)
				//}),
			)
		}),
		widgets.DrawLineFlex(widgets.Gray300, unit.Dp(1), unit.Dp(gtx.Constraints.Max.Y)),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			switch r.responseTabs.Selected() {
			case 1:
				return r.responseKeyValue(gtx, theme, r.responseHeadersList, "headers", r.responseHeaders)
			case 2:
				return r.responseKeyValue(gtx, theme, r.responseCookiesList, "cookies", r.responseCookies)
			default:
				return layout.Inset{Left: unit.Dp(5), Bottom: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return r.jsonViewer.Layout(gtx, theme)
				})
			}
		}),
	)
}

func (r *RestContainer) Layout(gtx layout.Context, theme *material.Theme) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{
				Left:   unit.Dp(10),
				Top:    unit.Dp(10),
				Bottom: unit.Dp(5),
			}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return material.Label(theme, theme.TextSize, "Create user").Layout(gtx)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(10)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return r.requestBar(gtx, theme)
			})
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return r.split.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return r.requestLayout(gtx, theme)
				},
				func(gtx layout.Context) layout.Dimensions {
					if r.loading {
						return material.Label(theme, theme.TextSize, "Loading...").Layout(gtx)
					} else {
						// update only once
						if !r.resultUpdated {
							r.jsonViewer.SetData(r.result)
							r.resultUpdated = true
						}
					}

					return r.responseLayout(gtx, theme)
				},
			)
		}),
	)
}
