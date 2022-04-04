package driver

import (
	"syscall"
	"testing"

	mock_netlinkwrapper "github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper/mocks"
	"github.com/golang/mock/gomock"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/vishvananda/netlink"
)

func Test_netLinkRuleDelAll(t *testing.T) {
	testRule := netlink.NewRule()
	testRule.IifName = "eni00bcc08c834"
	testRule.Priority = vlanRulePriority

	type ruleDelCall struct {
		rule *netlink.Rule
		err  error
	}

	type fields struct {
		ruleDelCalls []ruleDelCall
	}

	type args struct {
		rule *netlink.Rule
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr error
	}{
		{
			name: "single rule, succeed to delete",
			fields: fields{
				ruleDelCalls: []ruleDelCall{
					{
						rule: testRule,
					},
					{
						rule: testRule,
						err:  syscall.ENOENT,
					},
				},
			},
			args: args{
				rule: testRule,
			},
		},
		{
			name: "single rule, failed to delete",
			fields: fields{
				ruleDelCalls: []ruleDelCall{
					{
						rule: testRule,
						err:  errors.New("some error"),
					},
				},
			},
			args: args{
				rule: testRule,
			},
			wantErr: errors.New("some error"),
		},
		{
			name: "multiple rules, succeed to delete",
			fields: fields{
				ruleDelCalls: []ruleDelCall{
					{
						rule: testRule,
					},
					{
						rule: testRule,
					},
					{
						rule: testRule,
						err:  syscall.ENOENT,
					},
				},
			},
			args: args{
				rule: testRule,
			},
		},
		{
			name: "multiple rules, failed to delete",
			fields: fields{
				ruleDelCalls: []ruleDelCall{
					{
						rule: testRule,
					},
					{
						rule: testRule,
						err:  errors.New("some error"),
					},
				},
			},
			args: args{
				rule: testRule,
			},
			wantErr: errors.New("some error"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			netLink := mock_netlinkwrapper.NewMockNetLink(ctrl)
			for _, call := range tt.fields.ruleDelCalls {
				netLink.EXPECT().RuleDel(call.rule).Return(call.err)
			}

			err := netLinkRuleDelAll(netLink, tt.args.rule)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func Test_containsNoSuchRule(t *testing.T) {
	type args struct {
		err error
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "syscall.EEXIST is rule not exists error",
			args: args{
				err: syscall.ENOENT,
			},
			want: true,
		},
		{
			name: "syscall.ENOENT isn't rule not exists error",
			args: args{
				err: syscall.EEXIST,
			},
			want: false,
		},
		{
			name: "non syscall error isn't rule not exists error",
			args: args{
				err: errors.New("some error"),
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsNoSuchRule(tt.args.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_isRuleExistsError(t *testing.T) {
	type args struct {
		err error
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "syscall.EEXIST is rule exists error",
			args: args{
				err: syscall.EEXIST,
			},
			want: true,
		},
		{
			name: "syscall.ENOENT isn't rule exists error",
			args: args{
				err: syscall.ENOENT,
			},
			want: false,
		},
		{
			name: "non syscall error isn't rule exists error",
			args: args{
				err: errors.New("some error"),
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRuleExistsError(tt.args.err)
			assert.Equal(t, tt.want, got)
		})
	}
}
