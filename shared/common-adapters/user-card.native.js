// @flow

import React, {Component} from 'react-native'
import {globalStyles, globalColors} from '../styles/style-guide'
import type {Props} from './user-card'
import Avatar from './avatar'
import {View} from 'react-native'
import {Box} from '../common-adapters'
const avatarSize = 110

export default class UserCard extends Component<void, Props, void> {
  render () {
    console.log('in native user-card')
    const url = this.props.username ? `https://keybase.io/${this.props.username}` : null
    return (
      <Box style={{...styles.container, ...this.props.outerStyle}}>
        <Avatar size={avatarSize} style={styles.avatar} onClick={this.props.onAvatarClicked} url={url}/>
        <Box style={{...styles.inside, ...this.props.style}}>
          {this.props.children}
        </Box>
      </Box>
    )
  }
}

const styles = {
  container: {
    ...globalStyles.flexBoxColumn,
    alignItems: 'center',
    //width: 410,
    //height: 375,
    backgroundColor: globalColors.white,
    position: 'relative'
  },
  inside: {
    ...globalStyles.flexBoxColumn,
    alignItems: 'center',
    marginTop: avatarSize / 2,
    padding: 30
    //width: '100%',
    //height: '100%'
  },
  avatar: {
    position: 'absolute',
    top: -avatarSize / 2,
    left: 0,
    right: 0,
    marginLeft: 'auto',
    marginRight: 'auto'
  }
}
