# 用途

邮件发送服务

# 安装

    go get github.com/miraclesu/service

# 使用

    c := service.SmtpConf{
        "Host":     "smtp.gmail.com",
        "Port":     25,
        "Username": "sample@gmail.com",
        "Password": "password",
        "AuthType": "plain",
    }
    serv := service.New(l)
    s := service.NewSmtpServer(&c)
    if err := serv.AddServer(s); err != nil {
        //handler err
    }
    serv.Work()

    msg := service.Message{
        "SenderName" => "xx",
        "From" => "sample@gmail.com",
        "To" => map[string]string{"to_name":"name@xx.com"},
        "Subject" => "邮件标题——PHP测试",
        "Body" => "我是邮件xx, golang出品！",
        "Tag" => "smtp",
    }
    serv.Send(&msg)

# 配置文件参数说明

config.json配置参数说明：

Config为一个数组，每个元素为一台邮件服务器，服务器的配置说明：

* Host:     邮件服务器的smtp
* Port:     smtp的端口
* Username: 邮件服务器的发送帐户
* Password: 发送帐户的密码
* AuthType: smtp的验证方式
* TimeOut:   smtp协议中client连接的保持时间，单位s
* SickLimit: smtp服务器的超时时间，单位s
* SickStep:  检测smtp服务器有问题时给smtp加上的超时时间，单位s

# Authors

[miraclesu](suchuangji@gmail.com)

# Open Source - MIT Software License

