import * as React from 'react';
import Row from "react-bootstrap/Row";
import Col from "react-bootstrap/Col";
import {inject, observer} from "mobx-react";
import {ExplorerStore} from "app/stores/ExplorerStore";
import ListGroup from "react-bootstrap/ListGroup";

interface Props {
    explorerStore?: ExplorerStore;
}

@inject("explorerStore")
@observer
export class AnchorPayload extends React.Component<Props, any> {

    render() {
        let {payload} = this.props.explorerStore;
        return (
            payload &&
            <React.Fragment>
                <Row className={"mb-3"}>
                    <Col>
                        <ListGroup>
                            <ListGroup.Item>Version: {payload.version} </ListGroup.Item> 
                        </ListGroup>
                    </Col>
                    <Col>
                        <ListGroup>
                            <ListGroup.Item>ChildTangleID: {payload.childTangleID} </ListGroup.Item> 
                        </ListGroup>
                    </Col>
                    <Col>
                        <ListGroup>
                            <ListGroup.Item>LastStampID: {payload.lastStampID} </ListGroup.Item> 
                        </ListGroup>
                    </Col>
                    <Col>
                        <ListGroup>
                            <ListGroup.Item>ChildMessageID: {payload.childMessageID} </ListGroup.Item> 
                        </ListGroup>
                    </Col>
                    <Col>
                        <ListGroup>
                            <ListGroup.Item>MerkleRoot: {payload.merkleRoot} </ListGroup.Item> 
                        </ListGroup>
                    </Col>
                </Row>
            </React.Fragment>
        );
    }
}
